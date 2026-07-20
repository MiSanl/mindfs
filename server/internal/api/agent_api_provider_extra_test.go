package api

import (
	"errors"
	"reflect"
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
	hub.BindSessionClient("sess-2", "client-a")
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
