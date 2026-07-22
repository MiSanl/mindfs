package agent

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"mindfs/server/internal/agent/acp"
	"mindfs/server/internal/agent/claude"
	"mindfs/server/internal/agent/codex"
	agenttypes "mindfs/server/internal/agent/types"
)

// Pool routes agent session creation to protocol-specific runtimes.
type Pool struct {
	cfg        Config
	processCtx context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	sessions   map[string]*sessionEntry
	runtimeEnv map[string]map[string]string
	closed     bool
	acp        *acp.Runtime
	claude     *claude.Runtime
	codex      *codex.Runtime
}

type sessionEntry struct {
	agentName  string
	sessionKey string
	protocol   Protocol
	runtimeKey string
	session    agenttypes.Session
}

// RuntimeSessionInfo is a lightweight view of a live pool session.
type RuntimeSessionInfo struct {
	PoolKey      string
	AgentName    string
	SessionKey   string
	RuntimeKey   string
	Protocol     Protocol
	AgentSession string
}

// NewPool creates a new agent pool.
func NewPool(cfg Config) *Pool {
	processCtx, cancel := context.WithCancel(context.Background())
	return &Pool{
		cfg:        cfg,
		processCtx: processCtx,
		cancel:     cancel,
		sessions:   make(map[string]*sessionEntry),
		runtimeEnv: make(map[string]map[string]string),
		acp:        acp.NewRuntime(processCtx),
		claude:     claude.NewRuntime(),
		codex:      codex.NewRuntime(),
	}
}

// GetOrCreate returns an existing session handle or creates a new one.
func (p *Pool) GetOrCreate(ctx context.Context, in agenttypes.OpenSessionInput) (agenttypes.Session, error) {
	if in.SessionKey == "" {
		return nil, errors.New("session key required")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("agent pool closed")
	}
	if entry, ok := p.sessions[in.SessionKey]; ok && (in.RuntimeKey == "" || entry.runtimeKey == in.RuntimeKey) {
		p.mu.Unlock()
		return entry.session, nil
	}
	if entry, ok := p.sessions[in.SessionKey]; ok {
		delete(p.sessions, in.SessionKey)
		p.mu.Unlock()
		_ = entry.session.Close()
		p.mu.Lock()
	}
	def, ok := p.cfg.GetAgent(in.AgentName)
	if !ok {
		p.mu.Unlock()
		return nil, errors.New("agent not configured: " + in.AgentName)
	}
	protocol := def.Protocol
	if protocol == "" {
		protocol = DefaultProtocol(in.AgentName)
	}
	p.mu.Unlock()

	// openSession starts subprocesses and can be slow, so keep it outside the pool lock.
	sess, err := p.openSession(ctx, protocol, def, in)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = sess.Close()
		return nil, errors.New("agent pool closed")
	}
	// Another goroutine may have created the same session while the lock was released.
	if entry, ok := p.sessions[in.SessionKey]; ok && (in.RuntimeKey == "" || entry.runtimeKey == in.RuntimeKey) {
		existing := entry.session
		p.mu.Unlock()
		if protocol != ProtocolACP {
			_ = sess.Close()
		}
		return existing, nil
	}
	var replaced *sessionEntry
	if entry, ok := p.sessions[in.SessionKey]; ok {
		replaced = entry
		delete(p.sessions, in.SessionKey)
	}
	p.sessions[in.SessionKey] = &sessionEntry{
		agentName:  in.AgentName,
		sessionKey: in.SessionKey,
		protocol:   protocol,
		runtimeKey: in.RuntimeKey,
		session:    sess,
	}
	p.mu.Unlock()
	if replaced != nil && replaced.session != nil {
		_ = replaced.session.Close()
	}
	return sess, nil
}

func (p *Pool) openSession(ctx context.Context, protocol Protocol, def Definition, in agenttypes.OpenSessionInput) (agenttypes.Session, error) {
	env := cloneEnv(def.Env)
	if in.RuntimeEnv != nil {
		env = cloneEnv(in.RuntimeEnv)
	}
	args := append([]string{}, def.Args...)
	args = append(args, in.RuntimeArgs...)
	switch protocol {
	case ProtocolClaudeSDK:
		return p.claude.OpenSession(ctx, claude.OpenOptions{
			AgentName:       in.AgentName,
			SessionKey:      in.SessionKey,
			Model:           in.Model,
			Effort:          in.Effort,
			PlanMode:        in.PlanMode,
			RootPath:        in.RootPath,
			Command:         def.Command,
			Args:            args,
			Env:             env,
			ResumeSessionID: in.AgentSessionID,
			ForkSessionID:   in.ForkPoint.AgentSessionID,
			ResumeMessageID: in.ForkPoint.ClaudeMessageUUID,
		})
	case ProtocolCodexSDK:
		var codexUserOrdinal *int
		if in.ForkPoint.Kind == agenttypes.ForkPointCodexUserOrdinal {
			value := in.ForkPoint.CodexUserOrdinal
			codexUserOrdinal = &value
		}
		return p.codex.OpenSession(ctx, codex.OpenOptions{
			AgentName:        in.AgentName,
			SessionKey:       in.SessionKey,
			Model:            in.Model,
			Effort:           in.Effort,
			FastService:      in.FastService,
			PlanMode:         in.PlanMode,
			Probe:            in.Probe,
			RootPath:         in.RootPath,
			Command:          def.Command,
			Args:             args,
			Env:              env,
			RuntimeKey:       in.RuntimeKey,
			ResumeSessionID:  in.AgentSessionID,
			ForkSessionID:    in.ForkPoint.AgentSessionID,
			CodexUserOrdinal: codexUserOrdinal,
		})
	case ProtocolACP:
		fallthrough
	default:
		return p.acp.OpenSession(ctx, acp.OpenOptions{
			AgentName:       in.AgentName,
			SessionKey:      in.SessionKey,
			Model:           in.Model,
			Mode:            in.Mode,
			Effort:          in.Effort,
			RootPath:        in.RootPath,
			Command:         def.Command,
			Args:            append(def.BuildArgs(in.RootPath), in.RuntimeArgs...),
			Env:             env,
			Cwd:             def.ResolveCwd(in.RootPath),
			ResumeSessionID: in.AgentSessionID,
		})
	}
}

func (p *Pool) KillAgentProcess(agentName string, wait time.Duration) (string, bool) {
	_ = wait
	def, ok := p.cfg.GetAgent(agentName)
	if !ok {
		return "", false
	}

	protocol := def.Protocol
	if protocol == "" {
		protocol = DefaultProtocol(agentName)
	}
	switch protocol {
	case ProtocolClaudeSDK:
		p.closeSessionsForAgent(agentName, ProtocolClaudeSDK)
		log.Printf("[agent/pool] kill_agent_process.claude_closed agent=%s", agentName)
		return "", true
	case ProtocolCodexSDK:
		p.closeSessionsForAgent(agentName, ProtocolCodexSDK)
		_ = p.codex.Close(agentName)
		log.Printf("[agent/pool] kill_agent_process.codex_closed agent=%s", agentName)
		return "", true
	case ProtocolACP:
		p.closeSessionsForAgent(agentName, ProtocolACP)
		p.acp.Close(agentName)
		if hint, ok := p.acp.RecentCloseHint(agentName); ok {
			log.Printf("[agent/pool] kill_agent_process.hint agent=%s hint=%q", agentName, hint)
			return hint, true
		}
		log.Printf("[agent/pool] kill_agent_process.no_hint agent=%s", agentName)
		return "", false
	default:
		return "", false
	}
}

func (p *Pool) closeSessionsForAgent(agentName string, protocol Protocol) {
	p.closeSessions(
		p.takeSessions(func(entry *sessionEntry) bool {
			return entry.agentName == agentName && entry.protocol == protocol
		}),
	)
}

func cloneEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

// Close closes a session (not the underlying runtime pool).
func (p *Pool) Close(sessionKey string) {
	entries := p.takeSessions(func(entry *sessionEntry) bool {
		return entry.sessionKey == sessionKey
	})
	if len(entries) == 0 {
		return
	}
	p.closeSessions(entries)
	for _, entry := range entries {
		if entry.protocol == ProtocolACP {
			p.acp.CloseSession(sessionKey)
		}
	}
}

func (p *Pool) takeSessions(match func(*sessionEntry) bool) []*sessionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	var entries []*sessionEntry
	for key, entry := range p.sessions {
		if entry == nil || !match(entry) {
			continue
		}
		entries = append(entries, entry)
		delete(p.sessions, key)
	}
	return entries
}

func (p *Pool) closeSessions(entries []*sessionEntry) {
	for _, entry := range entries {
		if entry == nil || entry.session == nil {
			continue
		}
		_ = entry.session.Close()
	}
}

// Config returns the pool configuration.
func (p *Pool) Config() Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cfg
}

func (p *Pool) UpdateConfig(cfg Config) Config {
	p.mu.Lock()
	defer p.mu.Unlock()
	cfg = p.applyRuntimeEnvOverridesLocked(cfg)
	p.cfg = cfg
	return p.cfg
}

func (p *Pool) SetAgentEnv(agentName string, env map[string]string) error {
	if agentName == "" {
		return errors.New("agent required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.cfg.Agents {
		if p.cfg.Agents[i].Name != agentName {
			continue
		}
		p.runtimeEnv[agentName] = cloneEnv(env)
		p.cfg.Agents[i].Env = cloneEnv(env)
		return nil
	}
	return errors.New("agent not configured: " + agentName)
}

func (p *Pool) applyRuntimeEnvOverridesLocked(cfg Config) Config {
	if len(p.runtimeEnv) == 0 {
		return cfg
	}
	for i := range cfg.Agents {
		env, ok := p.runtimeEnv[cfg.Agents[i].Name]
		if !ok {
			continue
		}
		cfg.Agents[i].Env = cloneEnv(env)
	}
	return cfg
}

// Get returns an existing session handle if present.
func (p *Pool) Get(sessionKey string) (agenttypes.Session, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.sessions[sessionKey]
	if !ok || entry == nil || entry.session == nil {
		return nil, false
	}
	return entry.session, true
}

// GetRuntimeInfo returns metadata for a live pool session if present.
func (p *Pool) GetRuntimeInfo(sessionKey string) (RuntimeSessionInfo, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.sessions[sessionKey]
	if !ok || entry == nil || entry.session == nil {
		return RuntimeSessionInfo{}, false
	}
	info := RuntimeSessionInfo{
		PoolKey:    sessionKey,
		AgentName:  entry.agentName,
		SessionKey: entry.sessionKey,
		RuntimeKey: entry.runtimeKey,
		Protocol:   entry.protocol,
	}
	if sid := strings.TrimSpace(entry.session.SessionID()); sid != "" {
		info.AgentSession = sid
	}
	return info, true
}

// ListRuntimeInfoForMindFSSession returns live runtimes for a MindFS session key.
func (p *Pool) ListRuntimeInfoForMindFSSession(mindfsSessionKey string) []RuntimeSessionInfo {
	mindfsSessionKey = strings.TrimSpace(mindfsSessionKey)
	if mindfsSessionKey == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]RuntimeSessionInfo, 0)
	for poolKey, entry := range p.sessions {
		if entry == nil || entry.session == nil {
			continue
		}
		if poolKey != mindfsSessionKey && !strings.HasSuffix(poolKey, "-"+mindfsSessionKey) {
			continue
		}
		info := RuntimeSessionInfo{
			PoolKey:    poolKey,
			AgentName:  entry.agentName,
			SessionKey: mindfsSessionKey,
			RuntimeKey: entry.runtimeKey,
			Protocol:   entry.protocol,
		}
		if sid := strings.TrimSpace(entry.session.SessionID()); sid != "" {
			info.AgentSession = sid
		}
		out = append(out, info)
	}
	return out
}

func (p *Pool) RuntimeKey(sessionKey string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry := p.sessions[sessionKey]; entry != nil {
		return entry.runtimeKey
	}
	return ""
}

// Context returns the pool lifecycle context (read-only).
func (p *Pool) Context() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.processCtx != nil {
		return p.processCtx
	}
	return context.Background()
}

// CloseAll closes all runtime resources.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	p.closed = true
	p.sessions = make(map[string]*sessionEntry)
	cancel := p.cancel
	p.cancel = nil
	acpRuntime := p.acp
	claudeRuntime := p.claude
	codexRuntime := p.codex
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if acpRuntime != nil {
		acpRuntime.CloseAll()
	}
	if claudeRuntime != nil {
		claudeRuntime.CloseAll()
	}
	if codexRuntime != nil {
		codexRuntime.CloseAll()
	}
}
