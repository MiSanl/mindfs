package usecase

import (
	"errors"
	"strings"
	"testing"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/preferences"
	"mindfs/server/internal/session"
)

func TestModelIDInCatalogExactAndSuffix(t *testing.T) {
	models := []agenttypes.ModelInfo{
		{ID: "Acme/grok-4.5"},
		{ID: "gpt-4o"},
	}
	if !modelIDInCatalog(models, "grok-4.5") {
		t.Fatal("bare id should match provider/model suffix")
	}
	if !modelIDInCatalog(models, "Acme/grok-4.5") {
		t.Fatal("exact id should match")
	}
	if !modelIDInCatalog(models, "gpt-4o") {
		t.Fatal("exact bare catalog id should match")
	}
	if modelIDInCatalog(models, "claude-opus") {
		t.Fatal("unknown model must not match")
	}
	if modelIDInCatalog(nil, "gpt-4o") {
		t.Fatal("empty catalog must not match")
	}
}

func TestAgentAPIProviderModelAllowedHookDefaultNilSafe(t *testing.T) {
	prev := AgentAPIProviderModelAllowed
	t.Cleanup(func() { AgentAPIProviderModelAllowed = prev })
	AgentAPIProviderModelAllowed = nil
	// Production code checks nil before call; ensure we can set/restore hook.
	AgentAPIProviderModelAllowed = func(agentName, model string, lastConfig any) bool {
		return agentName == "codex" && model == "grok-4.5"
	}
	if !AgentAPIProviderModelAllowed("codex", "grok-4.5", nil) {
		t.Fatal("expected allow")
	}
	if AgentAPIProviderModelAllowed("codex", "gpt-5.5", nil) {
		t.Fatal("expected deny")
	}
}

func TestValidateAgentModelSkipsWhenRegistryOrStatusMissing(t *testing.T) {
	svc := &Service{Registry: nil}
	if err := svc.validateAgentModel("codex", "gpt-4o"); err != nil {
		t.Fatalf("nil registry should skip: %v", err)
	}
	if err := svc.validateAgentModel("", "gpt-4o"); err != nil {
		t.Fatalf("empty agent should skip: %v", err)
	}
	if err := svc.validateAgentModel("codex", ""); err != nil {
		t.Fatalf("empty model should skip: %v", err)
	}

	prober := agent.NewProber(nil, nil, 0)
	svc = &Service{Registry: stubValidateRegistry{prober: prober}}
	if err := svc.validateAgentModel("codex", "gpt-4o"); err != nil {
		t.Fatalf("missing status should allow: %v", err)
	}
}

func TestValidateAgentModelEmptyCatalogAllows(t *testing.T) {
	prober := agent.NewProber(nil, nil, 0)
	prober.SeedStatusForTest(agent.Status{
		Name:   "codex",
		Models: nil,
	})
	svc := &Service{Registry: stubValidateRegistry{prober: prober}}
	if err := svc.validateAgentModel("codex", "custom-model"); err != nil {
		t.Fatalf("empty catalog should allow: %v", err)
	}
}

func TestValidateAgentModelCatalogExactAndSuffix(t *testing.T) {
	prober := agent.NewProber(nil, nil, 0)
	prober.SeedStatusForTest(agent.Status{
		Name: "opencode",
		Models: []agenttypes.ModelInfo{
			{ID: "Acme/grok-4.5"},
			{ID: "gpt-4o"},
		},
	})
	svc := &Service{Registry: stubValidateRegistry{prober: prober}}
	if err := svc.validateAgentModel("opencode", "grok-4.5"); err != nil {
		t.Fatalf("suffix match should allow: %v", err)
	}
	if err := svc.validateAgentModel("opencode", "Acme/grok-4.5"); err != nil {
		t.Fatalf("exact match should allow: %v", err)
	}
	if err := svc.validateAgentModel("opencode", "gpt-4o"); err != nil {
		t.Fatalf("bare exact should allow: %v", err)
	}
}

func TestValidateAgentModelDeniesUnknownWithoutProviderHook(t *testing.T) {
	prev := AgentAPIProviderModelAllowed
	t.Cleanup(func() { AgentAPIProviderModelAllowed = prev })
	AgentAPIProviderModelAllowed = nil

	prober := agent.NewProber(nil, nil, 0)
	prober.SeedStatusForTest(agent.Status{
		Name:   "codex",
		Models: []agenttypes.ModelInfo{{ID: "gpt-4o"}},
	})
	svc := &Service{Registry: stubValidateRegistry{prober: prober}}
	err := svc.validateAgentModel("codex", "grok-4.5")
	if err == nil {
		t.Fatal("expected denial for unknown model")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateAgentModelAllowsViaActiveProviderHook(t *testing.T) {
	prev := AgentAPIProviderModelAllowed
	t.Cleanup(func() { AgentAPIProviderModelAllowed = prev })

	prober := agent.NewProber(nil, nil, 0)
	last := preferences.LastConfigSelection{Type: "api_provider", ID: "api-grok", Name: "Grok Relay"}
	prober.SeedStatusForTest(agent.Status{
		Name:                "codex",
		Models:              []agenttypes.ModelInfo{{ID: "gpt-4o"}}, // stale native catalog
		LastConfigSelection: last,
	})

	AgentAPIProviderModelAllowed = func(agentName, model string, lastConfig any) bool {
		if agentName != "codex" || model != "grok-4.5" {
			return false
		}
		sel, ok := lastConfig.(preferences.LastConfigSelection)
		return ok && sel.Type == "api_provider" && sel.ID == "api-grok"
	}

	svc := &Service{Registry: stubValidateRegistry{prober: prober}}
	if err := svc.validateAgentModel("codex", "grok-4.5"); err != nil {
		t.Fatalf("active provider hook should allow grok-4.5: %v", err)
	}
	if err := svc.validateAgentModel("codex", "totally-unknown"); err == nil {
		t.Fatal("unknown model still denied when hook returns false")
	}
}

func TestValidateAgentModelBackupLastConfigDoesNotAllowViaHook(t *testing.T) {
	prev := AgentAPIProviderModelAllowed
	t.Cleanup(func() { AgentAPIProviderModelAllowed = prev })

	prober := agent.NewProber(nil, nil, 0)
	prober.SeedStatusForTest(agent.Status{
		Name:   "codex",
		Models: []agenttypes.ModelInfo{{ID: "gpt-4o"}},
		LastConfigSelection: preferences.LastConfigSelection{
			Type: "backup",
			ID:   "bak-1",
		},
	})
	// Hook mirrors production: only api_provider last_config allows.
	AgentAPIProviderModelAllowed = func(agentName, model string, lastConfig any) bool {
		sel, ok := lastConfig.(preferences.LastConfigSelection)
		if !ok || sel.Type != "api_provider" {
			return false
		}
		return model == "grok-4.5"
	}
	svc := &Service{Registry: stubValidateRegistry{prober: prober}}
	if err := svc.validateAgentModel("codex", "grok-4.5"); err == nil {
		t.Fatal("backup last_config must not allow provider models")
	}
}

func TestValidateAgentModelForProviderDoesNotUseGlobalSelection(t *testing.T) {
	svc := &Service{}
	provider := &SessionProviderConfig{ID: "api-bound", Models: []string{"grok-4.5"}}
	if err := svc.validateAgentModelForProvider("codex", "grok-4.5", provider); err != nil {
		t.Fatalf("bound provider model rejected: %v", err)
	}
	if err := svc.validateAgentModelForProvider("codex", "gpt-5.5", provider); err == nil {
		t.Fatal("model absent from bound provider must be rejected")
	}
}

func TestResolveSessionSettingsForAgentDoesNotReuseAnotherAgent(t *testing.T) {
	current := &session.Session{
		Model: "claude-global",
		Exchanges: []session.Exchange{
			{Agent: "claude", Model: "claude-3-7", Mode: "plan", Effort: "high", FastService: "on"},
			{Agent: "codex", Model: "gpt-5.5", Mode: "code", Effort: "medium", FastService: "off"},
		},
	}
	if got := resolveSessionExchangeModelForAgent(current, "claude"); got != "claude-3-7" {
		t.Fatalf("claude model = %q", got)
	}
	if got := resolveSessionExchangeModeForAgent(current, "claude"); got != "plan" {
		t.Fatalf("claude mode = %q", got)
	}
	if got := resolveSessionExchangeEffortForAgent(current, "claude"); got != "high" {
		t.Fatalf("claude effort = %q", got)
	}
	if got := resolveSessionExchangeFastServiceForAgent(current, "claude"); got != "on" {
		t.Fatalf("claude fast service = %q", got)
	}
	if got := resolveRuntimeModel("opencode", current, nil, ""); got != "" {
		t.Fatalf("new agent inherited model = %q", got)
	}
	if got := resolveRuntimeMode("opencode", current, ""); got != "" {
		t.Fatalf("new agent inherited mode = %q", got)
	}
	if got := resolveRuntimeModel("codex", current, nil, ""); got != "gpt-5.5" {
		t.Fatalf("codex fallback model = %q", got)
	}
	if got := resolveRuntimeEffort("opencode", current, ""); got != "" {
		t.Fatalf("new agent inherited effort = %q", got)
	}
	if got := resolveRuntimeFastService("codex", current, ""); got != "off" {
		t.Fatalf("codex fast service = %q", got)
	}
}

// stubValidateRegistry implements Registry with only GetProber/GetPreferences meaningful.
type stubValidateRegistry struct {
	prober *agent.Prober
	prefs  *preferences.Store
}

func (r stubValidateRegistry) GetRoot(string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, errors.New("not implemented")
}
func (r stubValidateRegistry) GetSessionManager(string) (*session.Manager, error) {
	return nil, errors.New("not implemented")
}
func (r stubValidateRegistry) UpsertRoot(string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, errors.New("not implemented")
}
func (r stubValidateRegistry) RemoveRoot(string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, errors.New("not implemented")
}
func (r stubValidateRegistry) RenameRoot(string, string, string) (rootfs.RootInfo, error) {
	return rootfs.RootInfo{}, errors.New("not implemented")
}
func (r stubValidateRegistry) ListRoots() []rootfs.RootInfo       { return nil }
func (r stubValidateRegistry) GetAgentPool() *agent.Pool          { return nil }
func (r stubValidateRegistry) GetPreferences() *preferences.Store { return r.prefs }
func (r stubValidateRegistry) GetExternalSessionImporter(string) (agenttypes.ExternalSessionImporter, error) {
	return nil, errors.New("not implemented")
}
func (r stubValidateRegistry) GetProber() *agent.Prober { return r.prober }
func (r stubValidateRegistry) GetCandidateRegistry() *CandidateRegistry {
	return nil
}
func (r stubValidateRegistry) GetFileWatcher(string, *session.Manager) (*rootfs.SharedFileWatcher, error) {
	return nil, nil
}
func (r stubValidateRegistry) ReleaseFileWatcher(string, string) {}
