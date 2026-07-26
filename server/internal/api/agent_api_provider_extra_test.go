package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
)

func TestPromoteAnthropicProtocolGPTOnlyCatalogDoesNotPromote(t *testing.T) {
	modelsByProtocol := map[string][]string{
		apiProviderProtocolOpenAICompatible: {"gpt-5.5", "gpt-4o", "grok-4.5"},
	}
	promoteAnthropicProtocolFromOpenAI(modelsByProtocol, false)
	if len(modelsByProtocol[apiProviderProtocolAnthropicCompatible]) != 0 {
		t.Fatalf("GPT-only catalog must not promote anthropic, got %#v", modelsByProtocol[apiProviderProtocolAnthropicCompatible])
	}
	protocols := successfulProbeProtocols(modelsByProtocol)
	if apiProviderCompatibleWithAgent(agentAPIProvider{
		Protocols: protocols,
		Models:    modelsByProtocol[apiProviderProtocolOpenAICompatible],
	}, "claude") {
		t.Fatal("openai-only protocols must not be compatible with claude agent")
	}
}

func TestPromoteAnthropicProtocolOpenAIEndpointHint(t *testing.T) {
	// Catalog itself may not look Claude-y, but OpenAI probe reported anthropic endpoint types.
	modelsByProtocol := map[string][]string{
		apiProviderProtocolOpenAICompatible: {"relay-model-1", "relay-model-2"},
	}
	promoteAnthropicProtocolFromOpenAI(modelsByProtocol, true)
	if len(modelsByProtocol[apiProviderProtocolAnthropicCompatible]) != 2 {
		t.Fatalf("endpoint hint should promote: %#v", modelsByProtocol)
	}
	protocols := successfulProbeProtocols(modelsByProtocol)
	if !apiProviderCompatibleWithAgent(agentAPIProvider{Protocols: protocols}, "claude") {
		t.Fatal("endpoint-hint promotion should make provider claude-compatible")
	}
}

func TestPromoteAnthropicProtocolDoesNotOverwriteExistingAnthropic(t *testing.T) {
	modelsByProtocol := map[string][]string{
		apiProviderProtocolOpenAICompatible:    {"claude-sonnet-4-5", "gpt-4o"},
		apiProviderProtocolAnthropicCompatible: {"claude-from-native-probe"},
	}
	promoteAnthropicProtocolFromOpenAI(modelsByProtocol, true)
	if !reflect.DeepEqual(modelsByProtocol[apiProviderProtocolAnthropicCompatible], []string{"claude-from-native-probe"}) {
		t.Fatalf("must keep native anthropic catalog: %#v", modelsByProtocol[apiProviderProtocolAnthropicCompatible])
	}
}

func TestPromoteAnthropicProtocolNilSafe(t *testing.T) {
	promoteAnthropicProtocolFromOpenAI(nil, true)
	promoteAnthropicProtocolFromOpenAI(map[string][]string{}, true)
}

func TestProviderModelsSuggestAnthropicEdgeNames(t *testing.T) {
	// Bare sonnet/opus/haiku without claude/anthropic must NOT promote (too broad).
	if providerModelsSuggestAnthropic([]string{"sonnet-4-5-thinking"}) {
		t.Fatal("bare sonnet should not suggest anthropic")
	}
	if providerModelsSuggestAnthropic([]string{"opus-4-6"}) {
		t.Fatal("bare opus should not suggest anthropic")
	}
	if providerModelsSuggestAnthropic([]string{"haiku-3-5"}) {
		t.Fatal("bare haiku should not suggest anthropic")
	}
	if providerModelsSuggestAnthropic([]string{"router-opus-v2", "haiku-lite-internal"}) {
		t.Fatal("non-claude catalogs with opus/haiku tokens must not promote")
	}
	if !providerModelsSuggestAnthropic([]string{"claude-sonnet-4-5"}) {
		t.Fatal("claude-sonnet should suggest anthropic")
	}
	if providerModelsSuggestAnthropic(nil) {
		t.Fatal("empty catalog should not suggest")
	}
}

func TestApiProviderCompatibleWithAgentProtocols(t *testing.T) {
	openaiOnly := agentAPIProvider{Protocols: []string{apiProviderProtocolOpenAICompatible}}
	anthropicOnly := agentAPIProvider{Protocols: []string{apiProviderProtocolAnthropicCompatible}}
	both := agentAPIProvider{Protocols: []string{apiProviderProtocolOpenAICompatible, apiProviderProtocolAnthropicCompatible}}

	if apiProviderCompatibleWithAgent(openaiOnly, "claude") {
		t.Fatal("claude requires anthropic-compatible")
	}
	if !apiProviderCompatibleWithAgent(anthropicOnly, "claude") {
		t.Fatal("claude should accept anthropic-compatible")
	}
	if !apiProviderCompatibleWithAgent(openaiOnly, "codex") {
		t.Fatal("codex should accept openai-compatible")
	}
	if apiProviderCompatibleWithAgent(anthropicOnly, "codex") {
		t.Fatal("codex should not accept anthropic-only")
	}
	if !apiProviderCompatibleWithAgent(both, "opencode") {
		t.Fatal("opencode accepts either protocol")
	}
	if apiProviderCompatibleWithAgent(openaiOnly, "unknown-agent") {
		t.Fatal("unknown agent has no supported protocols")
	}
}

func TestOverlayMergesProbeEffortsBySuffixOnly(t *testing.T) {
	status := agent.Status{
		Name:           "codex",
		CurrentModelID: "stale",
		DefaultModelID: "stale",
		Models: []agenttypes.ModelInfo{{
			ID:            "provider/gpt-new",
			Name:          "Prefixed Probe",
			SupportEffort: true,
			Efforts:       []string{"medium", "xhigh"},
			DefaultEffort: "medium",
		}},
	}
	next := overlaySelectedProviderModels(status, agentAPIProvider{Name: "Demo", Models: []string{"gpt-new", "other"}})
	if len(next.Models) != 2 {
		t.Fatalf("models = %#v", next.Models)
	}
	got := next.Models[0]
	if got.ID != "gpt-new" {
		t.Fatalf("id = %q", got.ID)
	}
	if !got.SupportEffort || !reflect.DeepEqual(got.Efforts, []string{"medium", "xhigh"}) || got.DefaultEffort != "medium" {
		t.Fatalf("suffix merge efforts failed: %#v", got)
	}
	if !reflect.DeepEqual(next.Efforts, []string{"medium", "xhigh"}) {
		t.Fatalf("agent efforts = %#v", next.Efforts)
	}
	if next.Models[1].SupportEffort || len(next.Models[1].Efforts) != 0 {
		t.Fatalf("unmatched model should not inherit efforts: %#v", next.Models[1])
	}
}

func TestOverlayEmptyProviderModelsKeepsProbeAndCurrent(t *testing.T) {
	status := agent.Status{
		Name:           "claude",
		CurrentModelID: "native",
		DefaultModelID: "native",
		Models:         []agenttypes.ModelInfo{{ID: "native", Name: "Native", SupportEffort: true, Efforts: []string{"high"}}},
		Efforts:        []string{"high"},
	}
	next := overlaySelectedProviderModels(status, agentAPIProvider{Models: nil})
	if len(next.Models) != 1 || next.Models[0].ID != "native" || next.CurrentModelID != "native" {
		t.Fatalf("empty provider models must keep probe intact: %#v", next)
	}
	next = overlaySelectedProviderModels(status, agentAPIProvider{Models: []string{}})
	if len(next.Models) != 1 || next.Models[0].ID != "native" {
		t.Fatalf("empty slice should keep probe: %#v", next.Models)
	}
}

func TestAgentModelAllowedByActiveProviderMapLastConfig(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:     "api-grok",
		Name:   "Grok Relay",
		Models: []string{"grok-4.5"},
	}}); err != nil {
		t.Fatal(err)
	}
	lastMap := map[string]any{"type": "api_provider", "id": "api-grok", "name": "Grok Relay"}
	if !agentModelAllowedByActiveProvider("codex", "grok-4.5", lastMap) {
		t.Fatal("map last_config should allow provider model")
	}
	if agentModelAllowedByActiveProvider("codex", "missing", lastMap) {
		t.Fatal("unknown model denied")
	}
	lastStr := map[string]string{"type": "api_provider", "id": "api-grok"}
	if !agentModelAllowedByActiveProvider("codex", "grok-4.5", lastStr) {
		t.Fatal("map[string]string last_config should allow")
	}
}

func TestNormalizeAgentErrorMessage(t *testing.T) {
	if got := normalizeAgentErrorMessage(nil); got != "Unknown error" {
		t.Fatalf("nil = %q", got)
	}
	if got := normalizeAgentErrorMessage(errors.New("   ")); got != "Unknown error" {
		t.Fatalf("blank = %q", got)
	}
	if got := normalizeAgentErrorMessage(errors.New("plain failure")); got != "plain failure" {
		t.Fatalf("plain = %q", got)
	}
	if got := normalizeAgentErrorMessage(errors.New(`{"message":"rate limited"}`)); got != "rate limited" {
		t.Fatalf("json message = %q", got)
	}
	if got := normalizeAgentErrorMessage(errors.New(`{"code":"x"}`)); got != `{"code":"x"}` {
		t.Fatalf("json without message keeps raw: %q", got)
	}
}

func TestBroadcastSessionErrorWithRequestRecordsWithoutNetwork(t *testing.T) {
	app := &AppContext{}
	// No clients: BroadcastAll is a no-op; stream AppendReplyEvent still runs.
	app.BroadcastSessionErrorWithRequest("root-1", "sess-1", "req-42", `{"message":"provider timeout"}`)
	// Empty message defaults to "session error" after normalize path.
	app.BroadcastSessionError("root-1", "sess-1", "   ")
	// Bound client without websocket conn: SendToClient no-ops.
	hub := app.GetSessionStreamHub()
	hub.RegisterClient("client-a", nil)
	hub.BindSessionClient("root", "sess-2", "client-a")
	app.BroadcastSessionErrorWithRequest("root-1", "sess-2", "req-99", "boom")
}

func TestSuccessfulProbeProtocolsPriority(t *testing.T) {
	got := successfulProbeProtocols(map[string][]string{
		apiProviderProtocolGeminiCompatible:    {"g"},
		apiProviderProtocolAnthropicCompatible: {"a"},
		apiProviderProtocolOpenAICompatible:    {"o"},
	})
	want := []string{
		apiProviderProtocolOpenAICompatible,
		apiProviderProtocolAnthropicCompatible,
		apiProviderProtocolGeminiCompatible,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priority = %#v, want %#v", got, want)
	}
	if got := successfulProbeProtocols(map[string][]string{}); len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestCodexHomeDirPrefersCODEX_HOME(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "custom-codex"))
	got, err := codexHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(os.Getenv("CODEX_HOME")) {
		t.Fatalf("codexHomeDir=%q want CODEX_HOME", got)
	}
	cfg, err := codexConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cfg) != "config.toml" || filepath.Dir(cfg) != got {
		t.Fatalf("config path=%q", cfg)
	}
}

func TestSelectedProviderMatchesEffectiveCodexUsesCodexHome(t *testing.T) {
	dir := t.TempDir()
	codexDir := filepath.Join(dir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexDir)
	// Write a mismatched config (active provider points elsewhere).
	config := `model = "gpt"
model_provider = "other"

[model_providers.other]
name = "other"
base_url = "https://other.example/v1"
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, detail := selectedProviderMatchesEffective("codex", agentAPIProvider{
		ID:      "api-want",
		Name:    "want",
		BaseURL: "https://want.example",
	}, nil)
	if ok {
		t.Fatalf("expected mismatch, detail=%q", detail)
	}
	if !strings.Contains(detail, "model_provider=other") {
		t.Fatalf("detail=%q", detail)
	}
}

func TestOpenCodeConsistencyUsesNamedProviderBaseURL(t *testing.T) {
	dir := t.TempDir()
	// Windows UserHomeDir follows USERPROFILE.
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	cfgDir := filepath.Join(dir, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Multi-provider layout: no top-level baseURL; selected provider nested.
	cfg := map[string]any{
		"model": "sub2.bond.plus/gpt-5.5",
		"provider": map[string]any{
			"ilfy": map[string]any{
				"name": "ilfy",
				"options": map[string]any{
					"baseURL": "https://one.iflytek.com/api/llm/console/chat/v1",
					"apiKey":  "sk-ilfy",
				},
			},
			"sub2.bond.plus": map[string]any{
				"name": "sub2.bond.plus",
				"options": map[string]any{
					"baseURL": "https://sub2.freevinc.bond/v1",
					"apiKey":  "sk-sub2",
				},
			},
		},
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	ok, detail := selectedProviderMatchesEffective("opencode", agentAPIProvider{
		ID:      "api-sub2-bond-plus",
		Name:    "sub2.bond.plus",
		BaseURL: "https://sub2.freevinc.bond",
	}, nil)
	if !ok {
		t.Fatalf("expected match for nested provider baseURL, detail=%q", detail)
	}

	// Wrong nested base should mismatch.
	cfg["provider"].(map[string]any)["sub2.bond.plus"].(map[string]any)["options"].(map[string]any)["baseURL"] = "https://other.example/v1"
	raw, _ = json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ok, detail = selectedProviderMatchesEffective("opencode", agentAPIProvider{
		ID:      "api-sub2-bond-plus",
		Name:    "sub2.bond.plus",
		BaseURL: "https://sub2.freevinc.bond",
	}, nil)
	if ok {
		t.Fatal("expected mismatch when nested baseURL differs")
	}
	if !strings.Contains(detail, "sub2.bond.plus") {
		t.Fatalf("detail should mention provider name, got %q", detail)
	}
}


func TestOpenCodeRuntimeModelsPrefersSlugs(t *testing.T) {
	got := openCodeRuntimeModels([]string{
		"Composer 2.5 Fast",
		"Grok 4.5",
		"grok-composer-2.5-fast",
		"grok-4.5",
		"  ",
	})
	want := []string{"grok-composer-2.5-fast", "grok-4.5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	// Only spaced names remain if no slug exists.
	got = openCodeRuntimeModels([]string{"Composer 2.5 Fast", "Grok 4.5"})
	if !reflect.DeepEqual(got, []string{"Composer 2.5 Fast", "Grok 4.5"}) {
		t.Fatalf("spaced-only got %#v", got)
	}
	// An UNRELATED slug must not evict a spaced-only real model: the user would
	// silently lose "Composer 2.5 Fast" and get gpt-4o written as default.
	got = openCodeRuntimeModels([]string{"gpt-4o", "Composer 2.5 Fast"})
	if !reflect.DeepEqual(got, []string{"gpt-4o", "Composer 2.5 Fast"}) {
		t.Fatalf("unrelated slug evicted spaced model: %#v", got)
	}
	// Matching is pairwise: only the spaced name with a slug twin is dropped.
	got = openCodeRuntimeModels([]string{"Composer 2.5 Fast", "grok-composer-2.5-fast", "Solo Model X"})
	if !reflect.DeepEqual(got, []string{"grok-composer-2.5-fast", "Solo Model X"}) {
		t.Fatalf("pairwise filter wrong: %#v", got)
	}
}

func TestNormalizeAgentErrorMessageProviderAPI(t *testing.T) {
	got := normalizeAgentErrorMessage(errors.New(`{"code":-32603,"message":"Internal error: status 404","data":{"errorName":"APIError"}}`))
	if !strings.Contains(got, "404") || !strings.Contains(strings.ToLower(got), "model") {
		t.Fatalf("404 normalize=%q", got)
	}
	got = normalizeAgentErrorMessage(errors.New(`{"message":"Internal error: \"Upstream request failed\""}`))
	if !strings.Contains(strings.ToLower(got), "upstream") {
		t.Fatalf("upstream normalize=%q", got)
	}
}


func TestOverlayOpenCodeFiltersSpacedModels(t *testing.T) {
	status := agent.Status{
		Name:           "opencode",
		CurrentModelID: "my-cpa-grok/Composer 2.5 Fast",
		DefaultModelID: "my-cpa-grok/Composer 2.5 Fast",
		Models:         []agenttypes.ModelInfo{{ID: "stale", Name: "stale"}},
	}
	provider := agentAPIProvider{
		ID:   "api-cpa",
		Name: "my-cpa-grok",
		Models: []string{
			"Composer 2.5 Fast",
			"grok-composer-2.5-fast",
			"grok-4.5",
		},
	}
	out := overlaySelectedProviderModels(status, provider)
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		ids = append(ids, m.ID)
	}
	want := []string{"my-cpa-grok/grok-composer-2.5-fast", "my-cpa-grok/grok-4.5"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("picker models = %#v want %#v", ids, want)
	}
	// Spaced current model remapped onto first slug.
	if out.CurrentModelID != "my-cpa-grok/grok-composer-2.5-fast" {
		t.Fatalf("current=%q", out.CurrentModelID)
	}
	for _, id := range ids {
		if strings.Contains(id, " ") {
			t.Fatalf("spaced id leaked into picker: %q", id)
		}
	}
}
