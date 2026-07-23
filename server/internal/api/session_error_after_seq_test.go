package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

// TestPersistSessionErrorAfterSeqAttachRules covers the three attachment modes
// for durable session errors:
//  1. request_id present before StartPendingTurn → next user seq (len+1)
//  2. live pending turn → pending user seq
//  3. no request_id / no pending → last committed user seq
func TestPersistSessionErrorAfterSeqAttachRules(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "project")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(dir, "registry.json")
	registry := rootfs.NewRegistry(regPath)
	root, err := registry.Upsert(rootPath)
	if err != nil {
		t.Fatalf("upsert root: %v", err)
	}

	app := &AppContext{Dirs: registry}
	manager, err := app.GetSessionManager(root.ID)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown() })
	created, err := manager.Create(context.Background(), session.CreateInput{
		Type:  session.TypeChat,
		Name:  "after-seq",
		Agent: "claude",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Seed one completed user/agent pair (seq 1 user, seq 2 agent).
	now := time.Now().UTC()
	if err := manager.StartPendingTurn(context.Background(), created,
		session.Exchange{Seq: 1, Role: "user", Agent: "claude", Content: "first", Timestamp: now},
		session.Exchange{Seq: 2, Role: "agent", Agent: "claude", Content: "ok", Timestamp: now},
	); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if err := manager.CompletePendingTurn(context.Background(), created.Key); err != nil {
		t.Fatalf("complete seed: %v", err)
	}

	// 1) Pre-pending send-path failure (consistency check): request_id set, no pending.
	//    Must attach to next user seq = 3 (len(exchanges)=2 → +1).
	got := app.persistSessionError(root.ID, created.Key, "msg-pre", "session.message_failed", "turn", "provider mismatch", false)
	if got != 3 {
		t.Fatalf("pre-pending after_seq=%d want 3", got)
	}
	items, err := manager.ListSessionErrors(context.Background(), created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[len(items)-1].AfterSeq != 3 {
		t.Fatalf("stored pre-pending entry=%#v", items)
	}

	// 2) After StartPendingTurn for the next user message (seq 3): pending wins.
	current, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartPendingTurn(context.Background(), current,
		session.Exchange{Seq: 3, Role: "user", Agent: "claude", Content: "second", Timestamp: now},
		session.Exchange{Seq: 4, Role: "agent", Agent: "claude", Timestamp: now},
	); err != nil {
		t.Fatalf("start pending: %v", err)
	}
	got = app.persistSessionError(root.ID, created.Key, "msg-mid", "session.message_failed", "turn", "runtime boom", false)
	if got != 3 {
		t.Fatalf("pending after_seq=%d want 3", got)
	}

	// 3) Non-send failure (no request_id) with no pending: last committed user.
	//    Release pending without committing so history still ends at user seq 1.
	_ = manager.DiscardPendingTurn(context.Background(), created.Key)
	got = app.persistSessionError(root.ID, created.Key, "", "session.message_failed", "turn", "background fail", false)
	if got != 1 {
		t.Fatalf("background after_seq=%d want 1 (last committed user)", got)
	}
}
