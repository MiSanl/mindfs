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

// TestPersistSessionErrorAlwaysUserSeq: after_seq is always a USER exchange seq.
//  1. pending user → that user seq
//  2. no pending → last committed user seq
// Never invents next-seq-from-len and never attaches to agent seq.
func TestPersistSessionErrorAlwaysUserSeq(t *testing.T) {
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

	now := time.Now().UTC()
	// Seed completed user(1)/agent(2).
	if err := manager.StartPendingTurn(context.Background(), created,
		session.Exchange{Seq: 1, Role: "user", Agent: "claude", Content: "first", Timestamp: now},
		session.Exchange{Seq: 2, Role: "agent", Agent: "claude", Content: "ok", Timestamp: now},
	); err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	if err := manager.CompletePendingTurn(context.Background(), created.Key); err != nil {
		t.Fatalf("complete seed: %v", err)
	}

	// No pending: attach under last committed user (seq 1), not agent seq 2.
	got := app.persistSessionError(root.ID, created.Key, "msg-bg", "session.message_failed", "turn", "background", false)
	if got != 1 {
		t.Fatalf("no-pending after_seq=%d want 1 (last user)", got)
	}

	// Start next turn: user seq 3 pending.
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
		t.Fatalf("pending after_seq=%d want 3 (pending user)", got)
	}

	// Complete user-only (empty agent discarded) then error: still under user 3.
	if err := manager.CompletePendingTurn(context.Background(), created.Key); err != nil {
		t.Fatalf("complete user only: %v", err)
	}
	got = app.persistSessionError(root.ID, created.Key, "msg-after", "session.message_failed", "turn", "consistency", false)
	if got != 3 {
		t.Fatalf("after user-only complete after_seq=%d want 3", got)
	}
	items, err := manager.ListSessionErrors(context.Background(), created.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.AfterSeq%2 == 0 && item.AfterSeq > 0 {
			// Agent seqs in this fixture are even; errors must never use them.
			// User seqs are odd. (Not a universal invariant, but true for this test.)
			t.Fatalf("error attached to even/agent-like seq: %#v", item)
		}
	}
}
