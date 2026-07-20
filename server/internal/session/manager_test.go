package session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agenttypes "mindfs/server/internal/agent/types"
	rootfs "mindfs/server/internal/fs"
)

func newTestManager(t *testing.T, root rootfs.RootInfo) *Manager {
	t.Helper()
	manager := NewManager(root)
	t.Cleanup(func() { _ = manager.Shutdown() })
	return manager
}

func TestManagerUsesSessionDBLink(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	manager := newTestManager(t, root)

	linkedDB := filepath.Join(t.TempDir(), "session-list.db")
	linkFile := filepath.Join(root.MetaDir(), "sessions", "session-list.db.link")
	if err := writeSessionDBLink(linkFile, linkedDB); err != nil {
		t.Fatalf("write link: %v", err)
	}

	if _, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Linked"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root.MetaDir(), "sessions", "session-list.db")); err == nil {
		t.Fatalf("legacy session-list.db should not be created when link exists")
	}
	if _, err := os.Stat(linkedDB); err != nil {
		t.Fatalf("stat linked db: %v", err)
	}
}

func TestManagerProviderBindingSurvivesRuntimeStateUpdates(t *testing.T) {
	root := rootfs.NewRootInfo("provider-binding", "provider-binding", t.TempDir())
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Provider binding"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	bound, err := manager.BindProviderIfUnbound(context.Background(), AgentBinding{
		SessionKey:               created.Key,
		Agent:                    "codex",
		ProviderID:               "api-example",
		ProviderRevision:         "revision-1",
		ProviderEndpointRevision: "endpoint-1",
		ProviderProtocol:         "openai-compatible",
		ProviderState:            "available",
	})
	if err != nil {
		t.Fatalf("bind provider: %v", err)
	}
	if bound.ProviderID != "api-example" || bound.ProviderState != "available" {
		t.Fatalf("bound provider = %#v", bound)
	}

	if err := manager.UpdateAgentState(context.Background(), created, "codex", 7, "thread-1"); err != nil {
		t.Fatalf("update runtime state: %v", err)
	}
	loaded, err := manager.GetAgentBinding(context.Background(), created.Key, "codex")
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if loaded.AgentSessionID != "thread-1" || loaded.AgentCtxSeq != 7 {
		t.Fatalf("runtime state = %#v", loaded)
	}
	if loaded.ProviderID != "api-example" || loaded.ProviderRevision != "revision-1" || loaded.ProviderEndpointRevision != "endpoint-1" || loaded.ProviderProtocol != "openai-compatible" || loaded.ProviderState != "available" {
		t.Fatalf("provider state was overwritten: %#v", loaded)
	}

	bound, err = manager.BindProviderIfUnbound(context.Background(), AgentBinding{
		SessionKey: created.Key,
		Agent:      "codex",
		ProviderID: "api-other",
	})
	if err != nil {
		t.Fatalf("bind existing provider: %v", err)
	}
	if bound.ProviderID != "api-example" {
		t.Fatalf("provider id = %q, want original binding", bound.ProviderID)
	}
}

func TestManagerListsOnlyNonSecretProviderBindings(t *testing.T) {
	root := rootfs.NewRootInfo("provider-binding-list", "provider-binding-list", t.TempDir())
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Provider bindings"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := manager.BindProviderIfUnbound(context.Background(), AgentBinding{
		SessionKey:               created.Key,
		Agent:                    "claude",
		ProviderID:               "api-claude",
		ProviderRevision:         "revision",
		ProviderEndpointRevision: "endpoint",
		ProviderProtocol:         "anthropic-compatible",
		ProviderState:            "available",
	}); err != nil {
		t.Fatalf("bind provider: %v", err)
	}
	if err := manager.UpdateAgentState(context.Background(), created, "claude", 2, "claude-native-session"); err != nil {
		t.Fatalf("update runtime state: %v", err)
	}
	bindings, err := manager.ListAgentBindings(context.Background(), created.Key)
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("bindings = %#v", bindings)
	}
	if got := bindings[0]; got.Agent != "claude" || got.AgentSessionID != "claude-native-session" || got.ProviderID != "api-claude" {
		t.Fatalf("binding = %#v", got)
	}
}

func TestManagerCountsProviderBindings(t *testing.T) {
	root := rootfs.NewRootInfo("provider-count", "provider-count", t.TempDir())
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Provider count"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := manager.BindProviderIfUnbound(context.Background(), AgentBinding{SessionKey: created.Key, Agent: "codex", ProviderID: "api-count"}); err != nil {
		t.Fatalf("bind provider: %v", err)
	}
	if count, err := manager.CountProviderBindings(context.Background(), "api-count"); err != nil || count != 1 {
		t.Fatalf("count = %d, %v; want 1, nil", count, err)
	}
}

func TestManagerRecoversPendingTurnAfterRestart(t *testing.T) {
	root := rootfs.NewRootInfo("pending-recovery", "pending-recovery", t.TempDir())
	first := newTestManager(t, root)
	created, err := first.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Pending recovery"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC()
	if err := first.StartPendingTurn(context.Background(), created,
		Exchange{Seq: 1, Role: "user", Agent: "opencode", Content: "continue", Timestamp: now},
		Exchange{Seq: 2, Role: "agent", Agent: "opencode", Timestamp: now},
	); err != nil {
		t.Fatalf("start pending turn: %v", err)
	}
	if err := first.UpdatePendingTurn(context.Background(), created.Key, "partial answer\nwith progress", []ExchangeAux{{Seq: 2, ToolCall: &agenttypes.ToolCall{CallID: "tool-1", Title: "Run tests", Status: "complete"}}}); err != nil {
		t.Fatalf("update pending turn: %v", err)
	}
	if err := first.Shutdown(); err != nil {
		t.Fatalf("shutdown first manager: %v", err)
	}

	second := newTestManager(t, root)
	recovered, err := second.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("reopen session: %v", err)
	}
	if len(recovered.Exchanges) != 2 || recovered.Exchanges[0].Content != "continue" || recovered.Exchanges[1].Content != "partial answer\nwith progress" {
		t.Fatalf("recovered exchanges = %#v", recovered.Exchanges)
	}
	aux, err := second.GetExchangeAux(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("read recovered aux: %v", err)
	}
	if len(aux[2]) != 1 || aux[2][0].ToolCall == nil || aux[2][0].ToolCall.CallID != "tool-1" {
		t.Fatalf("recovered aux = %#v", aux)
	}
	if _, err := root.ReadMetaFile(filepath.ToSlash(filepath.Join("sessions", "pending", created.Key+".json"))); err == nil {
		t.Fatal("pending file remains after recovery")
	}
}

func TestManagerCompletesPendingTurnOnlyOnce(t *testing.T) {
	root := rootfs.NewRootInfo("pending-complete", "pending-complete", t.TempDir())
	manager := newTestManager(t, root)
	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Pending complete"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Now().UTC()
	if err := manager.StartPendingTurn(context.Background(), created,
		Exchange{Seq: 1, Role: "user", Agent: "opencode", Content: "hello", Timestamp: now},
		Exchange{Seq: 2, Role: "agent", Agent: "opencode", Content: "world", Timestamp: now},
	); err != nil {
		t.Fatalf("start pending turn: %v", err)
	}
	if err := manager.CompletePendingTurn(context.Background(), created.Key); err != nil {
		t.Fatalf("complete pending turn: %v", err)
	}
	if err := manager.CompletePendingTurn(context.Background(), created.Key); err != nil {
		t.Fatalf("complete pending turn twice: %v", err)
	}
	loaded, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(loaded.Exchanges) != 2 {
		t.Fatalf("exchange count = %d, want 2", len(loaded.Exchanges))
	}
}

func TestOpenSessionMetaDBMigratesLegacyAgentBindings(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "session-list.db")
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE session_agent_bindings (
		session_key TEXT NOT NULL,
		agent TEXT NOT NULL,
		agent_session_id TEXT NOT NULL,
		agent_ctx_seq INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (session_key, agent)
	)`); err != nil {
		db.Close()
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO session_agent_bindings (session_key, agent, agent_session_id, agent_ctx_seq) VALUES ('legacy', 'claude', 'native-1', 3)`); err != nil {
		db.Close()
		t.Fatalf("insert legacy binding: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	migrated, err := openSessionMetaDB(dbFile)
	if err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	defer migrated.Close()
	var binding AgentBinding
	if err := migrated.QueryRow(selectAgentBindingSQL, "legacy", "claude").Scan(
		&binding.SessionKey,
		&binding.Agent,
		&binding.AgentSessionID,
		&binding.AgentCtxSeq,
		&binding.ProviderID,
		&binding.ProviderRevision,
		&binding.ProviderEndpointRevision,
		&binding.ProviderProtocol,
		&binding.ProviderState,
	); err != nil {
		t.Fatalf("read migrated binding: %v", err)
	}
	if binding.ProviderID != "" || binding.ProviderRevision != "" || binding.ProviderEndpointRevision != "" || binding.ProviderProtocol != "" || binding.ProviderState != ProviderStateLegacyUnbound {
		t.Fatalf("legacy provider binding = %#v", binding)
	}
}

func TestAgentBindingSchemaRemainsReadableByPreviousColumns(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "session-list.db")
	db, err := openSessionMetaDB(dbFile)
	if err != nil {
		t.Fatalf("open upgraded db: %v", err)
	}
	if _, err := db.Exec(bindProviderIfUnboundSQL, "session", "codex", "api-current", "revision", "endpoint", "openai-compatible", "available"); err != nil {
		db.Close()
		t.Fatalf("write new binding: %v", err)
	}
	if _, err := db.Exec(upsertAgentBindingSQL, "session", "codex", "thread-1", 4); err != nil {
		db.Close()
		t.Fatalf("write runtime state: %v", err)
	}
	// This is the SELECT shape used by the pre-provider-binding binary. SQLite
	// accepts added columns, so a downgrade can still read its original state.
	var sessionKey, agent, nativeID string
	var contextSeq int
	if err := db.QueryRow(`SELECT session_key, agent, agent_session_id, agent_ctx_seq FROM session_agent_bindings WHERE session_key = ? AND agent = ?`, "session", "codex").Scan(&sessionKey, &agent, &nativeID, &contextSeq); err != nil {
		db.Close()
		t.Fatalf("read old column set: %v", err)
	}
	if sessionKey != "session" || agent != "codex" || nativeID != "thread-1" || contextSeq != 4 {
		db.Close()
		t.Fatalf("old column values = %q %q %q %d", sessionKey, agent, nativeID, contextSeq)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}

func TestManagerRecordRelatedWorktreeDoesNotOverwriteExisting(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Worktree"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	firstPath := filepath.Join(rootDir, "..", "mindfs-worktree-a")
	secondPath := filepath.Join(rootDir, "..", "mindfs-worktree-b")
	if added, err := manager.RecordRelatedWorktree(context.Background(), created.Key, root.ID, firstPath, "feature/a", "abc123"); err != nil {
		t.Fatalf("record first worktree: %v", err)
	} else if !added {
		t.Fatal("record first worktree added = false, want true")
	}
	if added, err := manager.RecordRelatedWorktree(context.Background(), created.Key, root.ID, secondPath, "feature/b", "def456"); err != nil {
		t.Fatalf("record second worktree: %v", err)
	} else if added {
		t.Fatal("record second worktree added = true, want false")
	}

	current, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if current.RelatedWorktree == nil {
		t.Fatal("RelatedWorktree is nil")
	}
	if current.RelatedWorktree.Path != filepath.Clean(firstPath) {
		t.Fatalf("RelatedWorktree.Path = %q, want %q", current.RelatedWorktree.Path, filepath.Clean(firstPath))
	}
	if current.RelatedWorktree.Branch != "feature/a" {
		t.Fatalf("RelatedWorktree.Branch = %q, want feature/a", current.RelatedWorktree.Branch)
	}
}

func TestManagerRelatedFilesAreScopedByRepoHeadAndPath(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Related repos"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	repoA := filepath.Join(rootDir, "repo-a")
	repoB := filepath.Join(rootDir, "repo-b")
	for _, repo := range []string{repoA, repoB} {
		if err := manager.RecordOutputFileInRepo(context.Background(), created.Key, root.ID, "git", repo, filepath.Base(repo), "src/main.go", "abc123"); err != nil {
			t.Fatalf("record related file %s: %v", repo, err)
		}
	}
	if err := manager.RecordOutputFileInRepo(context.Background(), created.Key, root.ID, "git", repoA, filepath.Base(repoA), "src/main.go", "abc123"); err != nil {
		t.Fatalf("record duplicate related file: %v", err)
	}

	current, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(current.RelatedFiles) != 2 {
		t.Fatalf("related files len = %d, want 2: %#v", len(current.RelatedFiles), current.RelatedFiles)
	}

	if err := manager.RemoveRelatedFileAtHead(context.Background(), created.Key, "src/main.go", "abc123", repoA, "git"); err != nil {
		t.Fatalf("remove related file: %v", err)
	}
	current, err = manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if len(current.RelatedFiles) != 1 {
		t.Fatalf("related files len after remove = %d, want 1: %#v", len(current.RelatedFiles), current.RelatedFiles)
	}
	if current.RelatedFiles[0].RepoPath != filepath.Clean(repoB) {
		t.Fatalf("remaining repo = %q, want %q", current.RelatedFiles[0].RepoPath, filepath.Clean(repoB))
	}
}

func TestManagerRecordsSubSessionRelatedFileOnParent(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	manager := newTestManager(t, root)

	parent, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Parent"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := manager.Create(context.Background(), CreateInput{
		Type:             TypeChat,
		ParentSessionKey: parent.Key,
		Name:             "Child",
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	repo := filepath.Join(rootDir, "repo")
	if err := manager.RecordOutputFileInRepo(context.Background(), child.Key, root.ID, "git", repo, filepath.Base(repo), "src/main.go", "abc123"); err != nil {
		t.Fatalf("record child related file: %v", err)
	}
	if err := manager.RecordOutputFileInRepo(context.Background(), child.Key, root.ID, "git", repo, filepath.Base(repo), "src/main.go", "abc123"); err != nil {
		t.Fatalf("record duplicate child related file: %v", err)
	}

	loadedChild, err := manager.Get(context.Background(), child.Key, 0)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	loadedParent, err := manager.Get(context.Background(), parent.Key, 0)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	for label, sess := range map[string]*Session{"child": loadedChild, "parent": loadedParent} {
		if len(sess.RelatedFiles) != 1 {
			t.Fatalf("%s related files len = %d, want 1: %#v", label, len(sess.RelatedFiles), sess.RelatedFiles)
		}
		file := sess.RelatedFiles[0]
		if file.Path != "src/main.go" || file.Head != "abc123" || file.RepoPath != filepath.Clean(repo) || file.RepoKind != "git" {
			t.Fatalf("%s related file = %#v", label, file)
		}
	}
}

func TestManagerFallsBackToUserDataSessionDBOnSQLitePanic(t *testing.T) {
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("panic-root", "panic-root", rootDir)
	manager := newTestManager(t, root)

	originalOpen := openSQLiteDB
	originalConfigDir := mindFSConfigDir
	defer func() {
		openSQLiteDB = originalOpen
		mindFSConfigDir = originalConfigDir
	}()
	configDir := t.TempDir()
	mindFSConfigDir = func() (string, error) {
		return configDir, nil
	}

	var opened []string
	openSQLiteDB = func(path string) (*sql.DB, error) {
		opened = append(opened, path)
		if strings.Contains(path, rootDir) {
			panic("sqlite legacy panic")
		}
		return originalOpen(path)
	}

	if _, err := manager.Create(context.Background(), CreateInput{Type: TypeChat, Name: "Fallback"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if len(opened) < 2 {
		t.Fatalf("opened paths = %#v, want legacy then fallback", opened)
	}
	linkFile := filepath.Join(root.MetaDir(), "sessions", "session-list.db.link")
	payload, err := root.ReadMetaFile("sessions/session-list.db.link")
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	linked := strings.TrimSpace(string(payload))
	if linked == "" || strings.Contains(linked, rootDir) {
		t.Fatalf("link target = %q, want user-data path", linked)
	}
	if got, ok, err := readSessionDBLink(linkFile); err != nil || !ok || got != linked {
		t.Fatalf("readSessionDBLink = %q, %v, %v; want %q, true, nil", got, ok, err, linked)
	}
}

func TestManagerPersistsParentSessionMetadata(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type:             TypeChat,
		ParentSessionKey: "parent-session",
		ParentToolCallID: "tool-call-1",
		Agent:            "codex",
		Model:            "gpt-test",
		Name:             "Subagent",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	loaded, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if loaded.ParentSessionKey != "parent-session" {
		t.Fatalf("ParentSessionKey = %q", loaded.ParentSessionKey)
	}
	if loaded.ParentToolCallID != "tool-call-1" {
		t.Fatalf("ParentToolCallID = %q", loaded.ParentToolCallID)
	}
}

func TestManagerPersistsExchangeModelDisplayName(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type:  TypeChat,
		Agent: "claude",
		Model: "opus",
		Name:  "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx := WithExchangeModelDisplayName(context.Background(), "glm-4.7")
	if err := manager.AddExchangeForAgent(ctx, created, "agent", "reply", "claude", "", "", ""); err != nil {
		t.Fatalf("add exchange: %v", err)
	}

	loaded, err := manager.Get(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(loaded.Exchanges) != 1 {
		t.Fatalf("exchange count = %d, want 1", len(loaded.Exchanges))
	}
	if got := loaded.Exchanges[0].Model; got != "opus" {
		t.Fatalf("exchange model = %q, want runtime id", got)
	}
	if got := loaded.Exchanges[0].ModelDisplayName; got != "glm-4.7" {
		t.Fatalf("exchange model display name = %q, want snapshot", got)
	}
}

func TestManagerStoresFullToolCallAndReturnsCompactedAux(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type: TypeChat,
		Name: "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	content := "full search output"
	err = manager.AddExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 1,
		ToolCall: &agenttypes.ToolCall{
			CallID:  "call-1",
			Title:   "search",
			Status:  "complete",
			Kind:    agenttypes.ToolKindSearch,
			Content: []agenttypes.ToolCallContentItem{{Type: "text", Text: content}},
			Meta:    map[string]any{"output": content, "query": "full"},
		},
	})
	if err != nil {
		t.Fatalf("add aux: %v", err)
	}

	aux, err := manager.GetExchangeAux(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get aux: %v", err)
	}
	if len(aux[2]) != 1 || aux[2][0].ToolCall == nil {
		t.Fatalf("aux[2] = %#v, want compacted toolcall", aux[2])
	}
	if len(aux[2][0].ToolCall.Content) != 0 {
		t.Fatalf("compacted content = %#v, want empty", aux[2][0].ToolCall.Content)
	}
	if output, ok := aux[2][0].ToolCall.Meta["output"]; ok {
		t.Fatalf("compacted meta output = %#v, want omitted", output)
	}
	if aux[2][0].ToolCall.Meta["query"] != "full" {
		t.Fatalf("compacted meta = %#v, want non-output keys preserved", aux[2][0].ToolCall.Meta)
	}

	toolCall, err := manager.GetFullToolCall(context.Background(), created.Key, "call-1")
	if err != nil {
		t.Fatalf("get full toolcall: %v", err)
	}
	if len(toolCall.Content) != 1 || !strings.Contains(toolCall.Content[0].Text, content) {
		t.Fatalf("full content = %#v, want %q", toolCall.Content, content)
	}
	if toolCall.Meta["output"] != content {
		t.Fatalf("full meta output = %#v, want %q", toolCall.Meta["output"], content)
	}
}

func TestManagerStoresPlanAndCompactAux(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type: TypeChat,
		Name: "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := manager.AddExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 0,
		Plan: &agenttypes.PlanUpdate{
			ID:      "plan-1",
			Content: "- inspect\n- patch",
		},
	}); err != nil {
		t.Fatalf("add plan aux: %v", err)
	}
	if err := manager.AddExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 0,
		Compact: &agenttypes.CompactNotice{
			ID:     "compact-1",
			Status: "complete",
		},
	}); err != nil {
		t.Fatalf("add compact aux: %v", err)
	}

	aux, err := manager.GetExchangeAux(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get aux: %v", err)
	}
	if len(aux[2]) != 2 {
		t.Fatalf("aux[2] length = %d, want 2: %#v", len(aux[2]), aux[2])
	}
	if aux[2][0].Plan == nil || aux[2][0].Plan.Content != "- inspect\n- patch" {
		t.Fatalf("plan aux = %#v", aux[2][0])
	}
	if aux[2][1].Compact == nil || aux[2][1].Compact.Status != "complete" {
		t.Fatalf("compact aux = %#v", aux[2][1])
	}
}

func TestManagerStoresTodoAux(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type: TypeChat,
		Name: "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := manager.AddExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 0,
		Todo: &agenttypes.TodoUpdate{
			Items: []agenttypes.TodoItem{{Content: "persist todos", Status: "in_progress"}},
		},
	}); err != nil {
		t.Fatalf("add todo aux: %v", err)
	}

	aux, err := manager.GetExchangeAux(context.Background(), created.Key, 0)
	if err != nil {
		t.Fatalf("get aux: %v", err)
	}
	if len(aux[2]) != 1 || aux[2][0].Todo == nil {
		t.Fatalf("aux[2] = %#v, want todo aux", aux[2])
	}
	if got := aux[2][0].Todo.Items[0].Content; got != "persist todos" {
		t.Fatalf("todo content = %q, want persist todos", got)
	}
}

func TestManagerGetFullToolCallReadsPendingAuxBeforeDisk(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type: TypeChat,
		Name: "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	callID := "call-pending"
	if err := manager.UpsertPendingExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 1,
		ToolCall: &agenttypes.ToolCall{
			CallID:  callID,
			Title:   "git diff",
			Status:  "running",
			Kind:    agenttypes.ToolKindExecute,
			Content: []agenttypes.ToolCallContentItem{{Type: "text", Text: "running output"}},
		},
	}); err != nil {
		t.Fatalf("upsert pending start: %v", err)
	}
	if err := manager.UpsertPendingExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 1,
		ToolCall: &agenttypes.ToolCall{
			CallID:  callID,
			Status:  "complete",
			Content: []agenttypes.ToolCallContentItem{{Type: "text", Text: "final diff output"}},
			Meta:    map[string]any{"outputBytes": 17},
		},
	}); err != nil {
		t.Fatalf("upsert pending final: %v", err)
	}

	toolCall, err := manager.GetFullToolCall(context.Background(), created.Key, callID)
	if err != nil {
		t.Fatalf("get pending full toolcall: %v", err)
	}
	if toolCall.Status != "complete" {
		t.Fatalf("status = %q, want complete", toolCall.Status)
	}
	if toolCall.Title != "git diff" {
		t.Fatalf("title = %q, want git diff", toolCall.Title)
	}
	if len(toolCall.Content) != 1 || toolCall.Content[0].Text != "final diff output" {
		t.Fatalf("content = %#v, want final diff output", toolCall.Content)
	}

	manager.ClearPendingExchangeAux(context.Background(), created.Key)
	if _, err := manager.GetFullToolCall(context.Background(), created.Key, callID); err == nil {
		t.Fatal("GetFullToolCall after clear returned nil error, want not found")
	}
}

func TestManagerMarkPendingAskUserAnsweredMergesAnswers(t *testing.T) {
	root := rootfs.NewRootInfo("mindfs", "mindfs", t.TempDir())
	manager := newTestManager(t, root)

	created, err := manager.Create(context.Background(), CreateInput{
		Type: TypeChat,
		Name: "Chat",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	callID := "ask-1"
	questions := []agenttypes.AskUserQuestionItem{{Question: "Pick one"}}
	if err := manager.UpsertPendingExchangeAux(context.Background(), created.Key, ExchangeAux{
		Seq:  2,
		Line: 0,
		ToolCall: &agenttypes.ToolCall{
			CallID: callID,
			Title:  "ask user",
			Status: "running",
			Kind:   agenttypes.ToolKindAskUser,
			Meta: map[string]any{
				"toolUseId": callID,
				"questions": questions,
			},
		},
	}); err != nil {
		t.Fatalf("upsert pending ask user: %v", err)
	}

	answeredAt := time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC)
	if err := manager.MarkPendingAskUserAnswered(context.Background(), created.Key, callID, map[string]string{
		"q_0": "Yes",
	}, answeredAt); err != nil {
		t.Fatalf("mark answered: %v", err)
	}

	toolCall, err := manager.GetFullToolCall(context.Background(), created.Key, callID)
	if err != nil {
		t.Fatalf("get full toolcall: %v", err)
	}
	if toolCall.Status != "complete" {
		t.Fatalf("status = %q, want complete", toolCall.Status)
	}
	if toolCall.Meta["questions"] == nil {
		t.Fatalf("questions were not preserved: %#v", toolCall.Meta)
	}
	answers, ok := toolCall.Meta["answers"].(map[string]string)
	if !ok || answers["q_0"] != "Yes" {
		t.Fatalf("answers = %#v, want q_0=Yes", toolCall.Meta["answers"])
	}
	if toolCall.Meta["answeredAt"] != answeredAt.Format(time.RFC3339Nano) {
		t.Fatalf("answeredAt = %#v, want %s", toolCall.Meta["answeredAt"], answeredAt.Format(time.RFC3339Nano))
	}
}
