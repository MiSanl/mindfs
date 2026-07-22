package session

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	rootfs "mindfs/server/internal/fs"
)

func TestAppendAndListSessionErrors(t *testing.T) {
	dir := t.TempDir()
	root := rootfs.NewRootInfo("error-log", "error-log", dir)
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "error-session"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := manager.AppendSessionError(context.Background(), created.Key, SessionError{
		RequestID:      "msg-1",
		AfterMessageID: "msg-1",
		AfterSeq:       2,
		Agent:          "opencode",
		Model:          "gpt-5.5",
		Code:           "session.message_failed",
		Kind:           "turn",
		Message:        "peer disconnected before response",
		Recoverable:    true,
		Timestamp:      time.Unix(1700000000, 0).UTC(),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	items, err := manager.ListSessionErrors(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len=%d want 1", len(items))
	}
	if items[0].AfterSeq != 2 || items[0].RequestID != "msg-1" {
		t.Fatalf("entry=%#v", items[0])
	}
	if items[0].Message != "peer disconnected before response" {
		t.Fatalf("message=%q", items[0].Message)
	}
	if items[0].ID == "" {
		t.Fatal("expected generated id")
	}
	metaPath := filepath.ToSlash(filepath.Join("sessions", "errors", created.Key+".jsonl"))
	if _, err := manager.Root().ReadMetaFile(metaPath); err != nil {
		t.Fatalf("read error file via meta: %v", err)
	}
}

func TestSessionErrorCapAndDeleteCleanup(t *testing.T) {
	dir := t.TempDir()
	root := rootfs.NewRootInfo("error-cap", "error-cap", dir)
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "error-cap-session"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < maxSessionErrorsKept+25; i++ {
		if err := manager.AppendSessionError(context.Background(), created.Key, SessionError{
			RequestID: fmt.Sprintf("msg-%d", i),
			AfterSeq:  i + 1,
			Message:   fmt.Sprintf("failure-%d", i),
			Timestamp: time.Unix(int64(1700000000+i), 0).UTC(),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	items, err := manager.ListSessionErrors(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != maxSessionErrorsKept {
		t.Fatalf("len=%d want %d", len(items), maxSessionErrorsKept)
	}
	if items[0].Message != fmt.Sprintf("failure-%d", 25) {
		t.Fatalf("oldest kept message=%q", items[0].Message)
	}
	if items[len(items)-1].Message != fmt.Sprintf("failure-%d", maxSessionErrorsKept+24) {
		t.Fatalf("newest message=%q", items[len(items)-1].Message)
	}
	if err := manager.Delete(context.Background(), created.Key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	metaPath := filepath.ToSlash(filepath.Join("sessions", "errors", created.Key+".jsonl"))
	if _, err := manager.Root().ReadMetaFile(metaPath); err == nil {
		t.Fatal("expected error log removed after delete")
	}
}
