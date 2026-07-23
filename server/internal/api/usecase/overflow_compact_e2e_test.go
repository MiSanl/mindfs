package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

// TestSendMessageContextOverflowCompactRetryLivePath exercises the real
// Service.SendMessage control flow with a scripted agent session:
// first prompt overflows → reopen → /compact → retry succeeds, and durable
// compact notices land in exchange aux. No live provider/network required.
func TestSendMessageContextOverflowCompactRetryLivePath(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("overflow-e2e", "overflow-e2e", rootDir)
	manager := newTestSessionManager(t, root)
	created, err := manager.Create(context.Background(), session.CreateInput{
		Type: session.TypeChat,
		Name: "overflow-compact-e2e",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	pool := agent.NewPool(agent.Config{
		Agents: []agent.Definition{{Name: "opencode", Command: "opencode"}},
	})
	t.Cleanup(func() {
		agent.OpenSessionHook = nil
		pool.CloseAll()
	})

	var (
		mu          sync.Mutex
		sendCalls   []string
		instanceN   int
		compactSeen []agenttypes.CompactNotice
	)

	agent.OpenSessionHook = func(_ context.Context, in agenttypes.OpenSessionInput) (agenttypes.Session, error) {
		mu.Lock()
		instanceN++
		id := fmt.Sprintf("fake-opencode-%d", instanceN)
		mu.Unlock()
		return &scriptedOverflowSession{
			id: id,
			send: func(ctx context.Context, content string, s *scriptedOverflowSession) error {
				mu.Lock()
				sendCalls = append(sendCalls, content)
				call := len(sendCalls)
				mu.Unlock()
				// First open / first user prompt: overflow before any assistant chunk.
				if call == 1 && !strings.HasPrefix(strings.TrimSpace(content), "/compact") {
					return errors.New("Prompt is too long for the model context window")
				}
				// Compact prompt on reopened session.
				if strings.HasPrefix(strings.TrimSpace(content), "/compact") {
					return nil
				}
				// Retry of original user prompt: stream a short reply.
				s.emit(agenttypes.Event{
					Type: agenttypes.EventTypeMessageChunk,
					Data: agenttypes.MessageChunk{Content: "COMPACT_RETRY_OK"},
				})
				s.emit(agenttypes.Event{
					Type: agenttypes.EventTypeMessageDone,
					Data: agenttypes.MessageDone{},
				})
				return nil
			},
		}, nil
	}

	registry := &commandTestRegistry{root: root, manager: manager, pool: pool}
	service := Service{Registry: registry}

	err = service.SendMessage(context.Background(), SendMessageInput{
		RootID:  root.ID,
		Key:     created.Key,
		Agent:   "opencode",
		Content: "please answer after compact",
		OnUpdate: func(ev agenttypes.Event) {
			if ev.Type == agenttypes.EventTypeCompact {
				if notice, ok := ev.Data.(agenttypes.CompactNotice); ok {
					mu.Lock()
					compactSeen = append(compactSeen, notice)
					mu.Unlock()
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	mu.Lock()
	calls := append([]string{}, sendCalls...)
	notices := append([]agenttypes.CompactNotice{}, compactSeen...)
	opens := instanceN
	mu.Unlock()

	if opens < 2 {
		t.Fatalf("expected reopen after overflow, opens=%d", opens)
	}
	if len(calls) < 3 {
		t.Fatalf("sendCalls=%#v want overflow + compact + retry", calls)
	}
	if strings.HasPrefix(strings.TrimSpace(calls[0]), "/compact") {
		t.Fatalf("first call should be user prompt, got %q", calls[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(calls[1]), "/compact") {
		t.Fatalf("second call should be compact, got %q", calls[1])
	}
	// Final call is the retried user prompt (may include switch/build wrappers).
	if !strings.Contains(calls[len(calls)-1], "please answer after compact") {
		t.Fatalf("retry prompt missing original content: %q", calls[len(calls)-1])
	}
	if len(notices) < 2 {
		t.Fatalf("compact WS notices=%#v want auto+complete", notices)
	}
	if notices[0].Status != "auto" {
		t.Fatalf("first notice status=%q", notices[0].Status)
	}
	foundComplete := false
	for _, n := range notices {
		if n.Status == "complete" {
			foundComplete = true
		}
	}
	if !foundComplete {
		t.Fatalf("missing complete compact notice: %#v", notices)
	}

	// Durable: completed history must include compact aux and assistant reply.
	got, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}
	joined := ""
	for _, ex := range got.Exchanges {
		joined += ex.Role + ":" + ex.Content + "\n"
	}
	if !strings.Contains(joined, "please answer after compact") {
		t.Fatalf("history missing user content: %s", joined)
	}
	if !strings.Contains(joined, "COMPACT_RETRY_OK") {
		t.Fatalf("history missing retry reply: %s", joined)
	}
	auxBySeq, err := manager.GetExchangeAux(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("GetExchangeAux: %v", err)
	}
	compactCount := 0
	for _, items := range auxBySeq {
		for _, item := range items {
			if item.Compact != nil {
				compactCount++
			}
		}
	}
	if compactCount < 2 {
		t.Fatalf("durable compact aux count=%d map=%#v", compactCount, auxBySeq)
	}
}

// scriptedOverflowSession is a fake agenttypes.Session for overflow compact e2e.
type scriptedOverflowSession struct {
	id       string
	onUpdate func(agenttypes.Event)
	send     func(ctx context.Context, content string, s *scriptedOverflowSession) error
	closed   bool
}

func (s *scriptedOverflowSession) SendMessage(ctx context.Context, content string) error {
	if s.send == nil {
		return nil
	}
	return s.send(ctx, content, s)
}
func (s *scriptedOverflowSession) AnswerQuestion(context.Context, agenttypes.AskUserAnswer) error {
	return nil
}
func (s *scriptedOverflowSession) CurrentModel() string                      { return "test-model" }
func (s *scriptedOverflowSession) SetModel(context.Context, string) error    { return nil }
func (s *scriptedOverflowSession) ListModels(context.Context) (agenttypes.ModelList, error) {
	return agenttypes.ModelList{}, nil
}
func (s *scriptedOverflowSession) SetMode(context.Context, string) error   { return nil }
func (s *scriptedOverflowSession) SetPlanMode(context.Context, bool) error { return nil }
func (s *scriptedOverflowSession) ListModes(context.Context) (agenttypes.ModeList, error) {
	return agenttypes.ModeList{}, nil
}
func (s *scriptedOverflowSession) ListCommands(context.Context) (agenttypes.CommandList, error) {
	return agenttypes.CommandList{}, nil
}
func (s *scriptedOverflowSession) CancelCurrentTurn() error { return nil }
func (s *scriptedOverflowSession) OnUpdate(onUpdate func(agenttypes.Event)) {
	s.onUpdate = onUpdate
}
func (s *scriptedOverflowSession) SessionID() string { return s.id }
func (s *scriptedOverflowSession) ContextWindow(context.Context) (agenttypes.ContextWindow, error) {
	return agenttypes.ContextWindow{}, nil
}
func (s *scriptedOverflowSession) Close() error {
	s.closed = true
	return nil
}
func (s *scriptedOverflowSession) emit(event agenttypes.Event) {
	if s.onUpdate != nil {
		s.onUpdate(event)
	}
}
