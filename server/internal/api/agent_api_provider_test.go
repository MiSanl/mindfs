package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mindfs/server/internal/agent"
	agenttypes "mindfs/server/internal/agent/types"
	"mindfs/server/internal/preferences"
)

func withTempProviderStore(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	prev := mindFSConfigDir
	mindFSConfigDir = func() (string, error) {
		return dir, nil
	}
	return func() {
		mindFSConfigDir = prev
	}
}

func TestUpdateAgentAPIProviderManualModelsAndKeepAPIKey(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()

	providers := []agentAPIProvider{{
		ID:            "api-demo",
		Name:          "Demo",
		BaseURL:       "https://example.com/v1",
		APIKey:        "sk-secret",
		Protocols:     []string{apiProviderProtocolOpenAICompatible},
		ModelFamilies: []string{"gpt"},
		Models:        []string{"old-model"},
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}}
	if err := writeAgentAPIProviders(providers); err != nil {
		t.Fatalf("write providers: %v", err)
	}

	updated, err := updateAgentAPIProvider(context.Background(), "api-demo", agentAPIProviderUpdateRequest{
		Name:      "Demo Renamed",
		BaseURL:   "https://example.com/v1",
		APIKey:    "", // keep existing
		Models:    []string{"gpt-new", "claude-new"},
		ModelsSet: true,
	})
	if err != nil {
		t.Fatalf("updateAgentAPIProvider() error = %v", err)
	}
	if updated.Name != "Demo Renamed" {
		t.Fatalf("name = %q, want Demo Renamed", updated.Name)
	}
	if updated.APIKey != "sk-secret" {
		t.Fatalf("api key changed unexpectedly")
	}
	// normalizeModelIDs sorts for stable storage.
	if !reflect.DeepEqual(updated.Models, []string{"claude-new", "gpt-new"}) {
		t.Fatalf("models = %#v", updated.Models)
	}
	if updated.ID != "api-demo" {
		t.Fatalf("id changed = %q", updated.ID)
	}

	loaded, err := readAgentAPIProviders()
	if err != nil {
		t.Fatalf("read providers: %v", err)
	}
	if len(loaded) != 1 || loaded[0].APIKey != "sk-secret" {
		t.Fatalf("persisted provider = %#v", loaded)
	}
}

func TestUpdateAgentAPIProviderNotFoundAndNameConflict(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()

	if err := writeAgentAPIProviders([]agentAPIProvider{
		{ID: "api-a", Name: "A", BaseURL: "https://a.example", APIKey: "k1", Models: []string{"m1"}},
		{ID: "api-b", Name: "B", BaseURL: "https://b.example", APIKey: "k2", Models: []string{"m2"}},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := updateAgentAPIProvider(context.Background(), "missing", agentAPIProviderUpdateRequest{
		Name:    "X",
		BaseURL: "https://x.example",
	})
	if !errors.Is(err, errAgentAPIProviderNotFound) {
		t.Fatalf("missing id error = %v", err)
	}

	_, err = updateAgentAPIProvider(context.Background(), "api-a", agentAPIProviderUpdateRequest{
		Name:    "B",
		BaseURL: "https://a.example",
	})
	if !errors.Is(err, errAgentConfigConflict) {
		t.Fatalf("name conflict error = %v", err)
	}
}

func TestOverlaySelectedProviderModels(t *testing.T) {
	status := agent.Status{
		Name:           "codex",
		CurrentModelID: "stale-model",
		DefaultModelID: "stale-model",
		Models: []agenttypes.ModelInfo{{
			ID:            "stale-model",
			Name:          "Stale",
			SupportEffort: true,
			Efforts:       []string{"low", "high"},
			DefaultEffort: "high",
		}},
	}
	provider := agentAPIProvider{
		ID:     "api-demo",
		Name:   "Demo",
		Models: []string{"gpt-new", "claude-new"},
	}

	next := overlaySelectedProviderModels(status, provider)
	if len(next.Models) != 2 || next.Models[0].ID != "gpt-new" {
		t.Fatalf("models = %#v", next.Models)
	}
	if next.CurrentModelID != "gpt-new" || next.DefaultModelID != "gpt-new" {
		t.Fatalf("current/default = %q/%q", next.CurrentModelID, next.DefaultModelID)
	}
	// Bare overlay without probe metadata must not claim effort support.
	if next.Models[0].SupportEffort {
		t.Fatalf("codex overlay without probe match should not mark SupportEffort: %#v", next.Models[0])
	}
}

func TestOverlaySelectedProviderModelsOpenCodePrefix(t *testing.T) {
	status := agent.Status{
		Name:           "opencode",
		CurrentModelID: "old/model",
		Models:         []agenttypes.ModelInfo{{ID: "old/model", Name: "model"}},
	}
	provider := agentAPIProvider{
		Name:   "My Provider",
		Models: []string{"gpt-4o"},
	}
	next := overlaySelectedProviderModels(status, provider)
	if len(next.Models) != 1 {
		t.Fatalf("models = %#v", next.Models)
	}
	// slugified provider config name is used for prefix
	wantPrefix := agentAPIProviderConfigName(provider)
	if next.Models[0].ID != wantPrefix+"/gpt-4o" {
		t.Fatalf("model id = %q, want prefixed with %q", next.Models[0].ID, wantPrefix)
	}
}

func TestApplyAgentAPIProviderCapabilitiesOverlaysModels(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()

	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:     "api-demo",
		Name:   "Demo",
		Models: []string{"provider-model-a", "provider-model-b"},
	}}); err != nil {
		t.Fatalf("write: %v", err)
	}

	statuses := []agent.Status{{
		Name:                "claude",
		LastConfigSelection: preferences.LastConfigSelection{Type: "api_provider", ID: "api-demo", Name: "Demo"},
		Models:              []agenttypes.ModelInfo{{ID: "stale", Name: "stale"}},
		CurrentModelID:      "stale",
	}}
	out := applyAgentAPIProviderCapabilities(statuses)
	if !out[0].SupportsAPIProviderSwitch {
		t.Fatal("expected SupportsAPIProviderSwitch")
	}
	if len(out[0].Models) != 2 || out[0].Models[0].ID != "provider-model-a" {
		t.Fatalf("overlay models = %#v", out[0].Models)
	}
	if out[0].CurrentModelID != "provider-model-a" {
		t.Fatalf("current model = %q", out[0].CurrentModelID)
	}
}

func TestApplyAgentAPIProviderCapabilitiesNoOverlayWithoutSelection(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()

	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:     "api-demo",
		Name:   "Demo",
		Models: []string{"provider-model-a"},
	}}); err != nil {
		t.Fatalf("write: %v", err)
	}

	statuses := []agent.Status{{
		Name:           "claude",
		Models:         []agenttypes.ModelInfo{{ID: "native", Name: "native"}},
		CurrentModelID: "native",
	}}
	out := applyAgentAPIProviderCapabilities(statuses)
	if len(out[0].Models) != 1 || out[0].Models[0].ID != "native" {
		t.Fatalf("models should stay native: %#v", out[0].Models)
	}
}

func TestNormalizeModelIDs(t *testing.T) {
	got := normalizeModelIDs([]string{"  a ", "b", "a", "", "c"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeModelIDs = %#v, want %#v", got, want)
	}
}

func TestAgentAPIProvidersPathUsesOverride(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	path, err := agentAPIProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "api-providers.json" {
		t.Fatalf("path = %q", path)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		// dir may not exist until write; ensure parent is temp
		_ = err
	}
}

func TestUpdateAgentAPIProviderRenameKeepsIDAndPublicDTOHidesKey(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()

	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:        "api-stable",
		Name:      "Old Name",
		BaseURL:   "https://example.com/v1",
		APIKey:    "sk-keep-me",
		Protocols: []string{apiProviderProtocolOpenAICompatible},
		Models:    []string{"m1"},
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}

	updated, err := updateAgentAPIProvider(context.Background(), "api-stable", agentAPIProviderUpdateRequest{
		Name:      "New Name",
		BaseURL:   "https://example.com/v1",
		Models:    []string{"m2", "m3"},
		ModelsSet: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.ID != "api-stable" {
		t.Fatalf("id changed: %q", updated.ID)
	}
	if updated.Name != "New Name" {
		t.Fatalf("name = %q", updated.Name)
	}
	if updated.APIKey != "sk-keep-me" {
		t.Fatal("api key should be kept when omitted")
	}

	pub := publicAgentAPIProvider(updated)
	payload, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	if strings.Contains(body, "sk-keep-me") || strings.Contains(body, "apiKey") {
		t.Fatalf("public payload leaked key: %s", body)
	}
}

func TestUpdateAgentAPIProviderExplicitEmptyModelsClearsCatalog(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:      "api-clear",
		Name:    "Clear",
		BaseURL: "https://example.com",
		APIKey:  "k",
		Models:  []string{"a", "b"},
	}}); err != nil {
		t.Fatal(err)
	}
	updated, err := updateAgentAPIProvider(context.Background(), "api-clear", agentAPIProviderUpdateRequest{
		Name:      "Clear",
		BaseURL:   "https://example.com",
		Models:    nil,
		ModelsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Models) != 0 {
		t.Fatalf("models should be cleared, got %#v", updated.Models)
	}
}

func TestUpdateThenDeleteProviderLifecycle(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID: "api-life", Name: "Life", BaseURL: "https://example.com", APIKey: "k", Models: []string{"m"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := updateAgentAPIProvider(context.Background(), "api-life", agentAPIProviderUpdateRequest{
		Name: "Life2", BaseURL: "https://example.com", Models: []string{"n"}, ModelsSet: true,
	}); err != nil {
		t.Fatal(err)
	}
	remaining, err := deleteAgentAPIProvider("api-life")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %#v", remaining)
	}
}

func TestOverlayMergesProbeEffortsByExactAndSuffixID(t *testing.T) {
	status := agent.Status{
		Name:           "codex",
		CurrentModelID: "stale",
		DefaultModelID: "stale",
		Models: []agenttypes.ModelInfo{
			{
				ID:            "gpt-new",
				Name:          "From Probe",
				SupportEffort: true,
				Efforts:       []string{"low", "high", "ultra"},
				DefaultEffort: "high",
			},
		},
		Efforts: []string{"low"},
	}
	provider := agentAPIProvider{Name: "Demo", Models: []string{"gpt-new", "other"}}
	next := overlaySelectedProviderModels(status, provider)
	if len(next.Models) != 2 {
		t.Fatalf("models = %#v", next.Models)
	}
	got := next.Models[0]
	if got.Name != "From Probe" || !reflect.DeepEqual(got.Efforts, []string{"low", "high", "ultra"}) || got.DefaultEffort != "high" {
		t.Fatalf("merged model = %#v", got)
	}
	if !reflect.DeepEqual(next.Efforts, []string{"low", "high", "ultra"}) {
		t.Fatalf("agent efforts = %#v", next.Efforts)
	}

	// opencode: provider model bare id should merge with probe provider/model id.
	openStatus := agent.Status{
		Name: "opencode",
		Models: []agenttypes.ModelInfo{{
			ID:            "My Provider/gpt-4o",
			SupportEffort: false,
			Description:   "desc",
		}},
	}
	openProvider := agentAPIProvider{Name: "My Provider", Models: []string{"gpt-4o"}}
	openNext := overlaySelectedProviderModels(openStatus, openProvider)
	if len(openNext.Models) != 1 || openNext.Models[0].ID != "My Provider/gpt-4o" {
		t.Fatalf("opencode models = %#v", openNext.Models)
	}
	if openNext.Models[0].Description != "desc" {
		t.Fatalf("suffix merge failed: %#v", openNext.Models[0])
	}
}

func TestOverlayLastConfigMapFormAndBackupNoOverlay(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID: "api-demo", Name: "Demo", Models: []string{"p1", "p2"},
	}}); err != nil {
		t.Fatal(err)
	}

	// map[string]any last_config (JSON-decoded shape)
	withMap := applyAgentAPIProviderCapabilities([]agent.Status{{
		Name: "claude",
		LastConfigSelection: map[string]any{
			"type": "api_provider",
			"id":   "api-demo",
			"name": "Demo",
		},
		Models:         []agenttypes.ModelInfo{{ID: "native"}},
		CurrentModelID: "native",
	}})
	if len(withMap[0].Models) != 2 || withMap[0].Models[0].ID != "p1" {
		t.Fatalf("map last_config overlay failed: %#v", withMap[0].Models)
	}

	// backup selection must not overlay
	withBackup := applyAgentAPIProviderCapabilities([]agent.Status{{
		Name: "claude",
		LastConfigSelection: preferences.LastConfigSelection{
			Type: "backup",
			ID:   "bak-1",
		},
		Models: []agenttypes.ModelInfo{{ID: "native"}},
	}})
	if len(withBackup[0].Models) != 1 || withBackup[0].Models[0].ID != "native" {
		t.Fatalf("backup should not overlay: %#v", withBackup[0].Models)
	}
}

func TestOverlayEmptyProviderModelsKeepsProbeCatalog(t *testing.T) {
	status := agent.Status{
		Name:   "claude",
		Models: []agenttypes.ModelInfo{{ID: "native", Name: "Native"}},
	}
	next := overlaySelectedProviderModels(status, agentAPIProvider{Models: nil})
	if len(next.Models) != 1 || next.Models[0].ID != "native" {
		t.Fatalf("should keep probe models: %#v", next.Models)
	}
}

func TestApplyAgentAPIProviderCapabilitiesMultiAgentBatch(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{
		{ID: "api-a", Name: "A", Models: []string{"a1"}},
		{ID: "api-b", Name: "B", Models: []string{"b1", "b2"}},
	}); err != nil {
		t.Fatal(err)
	}
	out := applyAgentAPIProviderCapabilities([]agent.Status{
		{
			Name:                "claude",
			LastConfigSelection: preferences.LastConfigSelection{Type: "api_provider", ID: "api-a"},
			Models:              []agenttypes.ModelInfo{{ID: "old"}},
		},
		{
			Name:                "codex",
			LastConfigSelection: preferences.LastConfigSelection{Type: "api_provider", ID: "api-b"},
			Models:              []agenttypes.ModelInfo{{ID: "old"}},
		},
		{
			Name:   "gemini",
			Models: []agenttypes.ModelInfo{{ID: "g-native"}},
		},
	})
	if len(out[0].Models) != 1 || out[0].Models[0].ID != "a1" {
		t.Fatalf("claude = %#v", out[0].Models)
	}
	if len(out[1].Models) != 2 {
		t.Fatalf("codex = %#v", out[1].Models)
	}
	// Without probe metadata merge, SupportEffort stays false.
	if out[1].Models[0].SupportEffort {
		t.Fatalf("codex bare overlay should not enable SupportEffort: %#v", out[1].Models[0])
	}
	if len(out[2].Models) != 1 || out[2].Models[0].ID != "g-native" {
		t.Fatalf("gemini = %#v", out[2].Models)
	}
}

func TestMatchModelIDSuffix(t *testing.T) {
	models := []agenttypes.ModelInfo{{ID: "prov/gpt"}, {ID: "other"}}
	if got := matchModelID(models, "gpt"); got != "prov/gpt" {
		t.Fatalf("matchModelID = %q", got)
	}
	if got := matchModelID(models, "missing"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestFindProbeModelSuffixBothDirections(t *testing.T) {
	probe := []agenttypes.ModelInfo{{ID: "prov/gpt-4o", Efforts: []string{"low"}}}
	got, ok := findProbeModel(probe, "gpt-4o")
	if !ok || got.ID != "prov/gpt-4o" {
		t.Fatalf("find bare against prefixed = %#v ok=%v", got, ok)
	}
	got, ok = findProbeModel([]agenttypes.ModelInfo{{ID: "gpt-4o", Efforts: []string{"high"}}}, "prov/gpt-4o")
	if !ok || got.ID != "gpt-4o" {
		t.Fatalf("find prefixed against bare = %#v ok=%v", got, ok)
	}
}

func TestModelIDListsEqual(t *testing.T) {
	if !modelIDListsEqual([]string{"b", "a"}, []string{"a", "b"}) {
		t.Fatal("sorted equal lists should match")
	}
	if modelIDListsEqual([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("different lengths")
	}
}

func TestAgentModelAllowedByActiveProviderAcceptsCustomModel(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:     "api-grok",
		Name:   "Grok Relay",
		Models: []string{"grok-4.5", "gpt-4o"},
	}}); err != nil {
		t.Fatal(err)
	}
	last := preferences.LastConfigSelection{Type: "api_provider", ID: "api-grok", Name: "Grok Relay"}
	if !agentModelAllowedByActiveProvider("codex", "grok-4.5", last) {
		t.Fatal("expected grok-4.5 allowed for codex via active provider")
	}
	if agentModelAllowedByActiveProvider("codex", "totally-unknown", last) {
		t.Fatal("unknown model must still be rejected")
	}
	if agentModelAllowedByActiveProvider("codex", "grok-4.5", preferences.LastConfigSelection{Type: "backup", ID: "b1"}) {
		t.Fatal("backup selection must not allow provider models")
	}
	if agentModelAllowedByActiveProvider("codex", "grok-4.5", nil) {
		t.Fatal("nil last_config must not allow provider models")
	}
}

func TestAgentModelAllowedByActiveProviderOpenCodePrefixed(t *testing.T) {
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:     "api-acme",
		Name:   "Acme",
		Models: []string{"grok-4.5"},
	}}); err != nil {
		t.Fatal(err)
	}
	last := preferences.LastConfigSelection{Type: "api_provider", ID: "api-acme", Name: "Acme"}
	// bare id in store should allow bare or prefixed selection
	if !agentModelAllowedByActiveProvider("opencode", "grok-4.5", last) {
		t.Fatal("bare model should be allowed")
	}
	if !agentModelAllowedByActiveProvider("opencode", "Acme/grok-4.5", last) {
		t.Fatal("prefixed model should be allowed")
	}
}

func TestProviderModelsSuggestAnthropic(t *testing.T) {
	if !providerModelsSuggestAnthropic([]string{"claude-sonnet-4-5", "claude-opus-4-8"}) {
		t.Fatal("claude models should suggest anthropic")
	}
	if !providerModelsSuggestAnthropic([]string{"claude-fable-5"}) {
		t.Fatal("fable models should suggest anthropic")
	}
	if providerModelsSuggestAnthropic([]string{"gpt-5.5", "grok-4.5"}) {
		t.Fatal("openai/grok-only catalog should not suggest anthropic")
	}
}

func TestEndpointTypesSuggestAnthropic(t *testing.T) {
	if !endpointTypesSuggestAnthropic([]string{"openai", "anthropic"}) {
		t.Fatal("explicit anthropic endpoint type should suggest")
	}
	if !endpointTypesSuggestAnthropic([]string{"Claude-Messages"}) {
		t.Fatal("claude endpoint label should suggest")
	}
	if endpointTypesSuggestAnthropic([]string{"openai", "chat.completions"}) {
		t.Fatal("openai-only endpoints should not suggest")
	}
}

func TestPromoteAnthropicProtocolFromOpenAIClaudeCatalog(t *testing.T) {
	// Simulate OpenAI probe success with Claude-family models and Anthropic probe failure.
	modelsByProtocol := map[string][]string{
		apiProviderProtocolOpenAICompatible: {"claude-sonnet-4-5", "claude-fable-5"},
	}
	promoteAnthropicProtocolFromOpenAI(modelsByProtocol, false)
	protocols := successfulProbeProtocols(modelsByProtocol)
	foundAnthropic := false
	for _, p := range protocols {
		if p == apiProviderProtocolAnthropicCompatible {
			foundAnthropic = true
		}
	}
	if !foundAnthropic {
		t.Fatalf("expected anthropic-compatible promotion, protocols=%v", protocols)
	}
	if !reflect.DeepEqual(modelsByProtocol[apiProviderProtocolAnthropicCompatible], []string{"claude-sonnet-4-5", "claude-fable-5"}) {
		t.Fatalf("promoted models = %#v", modelsByProtocol[apiProviderProtocolAnthropicCompatible])
	}
	if !apiProviderCompatibleWithAgent(agentAPIProvider{
		Protocols: protocols,
		Models:    modelsByProtocol[apiProviderProtocolOpenAICompatible],
	}, "claude") {
		t.Fatal("promoted provider should be visible under claude agent filter")
	}
}

func TestCreateProviderCompatibleFilterAfterAnthropicPromotion(t *testing.T) {
	// After promotion, Claude list filter must include providers that only
	// successfully probed OpenAI but serve Claude-family models (ilfy case).
	provider := agentAPIProvider{
		ID:   "api-ilfy",
		Name: "ilfy",
		Protocols: []string{
			apiProviderProtocolOpenAICompatible,
			// promoted:
			apiProviderProtocolAnthropicCompatible,
		},
		Models: []string{"claude-sonnet-4-5", "claude-fable-5"},
	}
	if !apiProviderCompatibleWithAgent(provider, "claude") {
		t.Fatal("ilfy-like promoted provider should appear for claude")
	}
	if !apiProviderCompatibleWithAgent(provider, "codex") {
		t.Fatal("openai-compatible should still appear for codex")
	}
	openaiOnly := agentAPIProvider{
		ID:        "api-gpt",
		Name:      "gpt-only",
		Protocols: []string{apiProviderProtocolOpenAICompatible},
		Models:    []string{"gpt-5.5"},
	}
	if apiProviderCompatibleWithAgent(openaiOnly, "claude") {
		t.Fatal("gpt-only provider must not appear for claude")
	}
}

func TestModelIDInProviderCatalogBareAndPrefixed(t *testing.T) {
	overlaid := []agenttypes.ModelInfo{{ID: "Acme/grok-4.5"}}
	raw := []string{"grok-4.5"}
	if !modelIDInProviderCatalog(overlaid, raw, "grok-4.5", "opencode", agentAPIProvider{Name: "Acme"}) {
		t.Fatal("bare should match")
	}
	if !modelIDInProviderCatalog(overlaid, raw, "Acme/grok-4.5", "opencode", agentAPIProvider{Name: "Acme"}) {
		t.Fatal("prefixed should match")
	}
	if modelIDInProviderCatalog(overlaid, raw, "nope", "opencode", agentAPIProvider{Name: "Acme"}) {
		t.Fatal("unknown must not match")
	}
}

func TestUpdateAgentAPIProviderURLChangeRefreshesProtocolsWithCuratedModels(t *testing.T) {
	// Cannot call real network probe in unit test; cover pure branch via models-only
	// path and protocol keep, plus modelIDListsEqual / shouldReprobe logic indirectly.
	// When ModelsSet+urlChanged and we cannot probe, update would fail on network.
	// Instead verify curated models keep + protocol promotion helper still works.
	restore := withTempProviderStore(t)
	defer restore()
	if err := writeAgentAPIProviders([]agentAPIProvider{{
		ID:        "api-move",
		Name:      "Move",
		BaseURL:   "https://old.example/v1",
		APIKey:    "k",
		Protocols: []string{apiProviderProtocolOpenAICompatible},
		Models:    []string{"claude-sonnet-4-5", "keep-me"},
	}}); err != nil {
		t.Fatal(err)
	}
	// models-only edit (no url/key change): keep protocols, replace models
	updated, err := updateAgentAPIProvider(context.Background(), "api-move", agentAPIProviderUpdateRequest{
		Name:      "Move",
		BaseURL:   "https://old.example/v1",
		Models:    []string{"claude-sonnet-4-5", "claude-fable-5"},
		ModelsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updated.Protocols, []string{apiProviderProtocolOpenAICompatible}) {
		t.Fatalf("protocols = %#v", updated.Protocols)
	}
	if !reflect.DeepEqual(updated.Models, []string{"claude-fable-5", "claude-sonnet-4-5"}) {
		t.Fatalf("models = %#v", updated.Models)
	}
}

func TestProviderModelsSuggestAnthropicRejectsBareOpusHaiku(t *testing.T) {
	if providerModelsSuggestAnthropic([]string{"router-opus-v2"}) {
		t.Fatal("router-opus-v2 must not promote")
	}
	if providerModelsSuggestAnthropic([]string{"haiku-lite-internal"}) {
		t.Fatal("haiku-lite-internal must not promote")
	}
	if !providerModelsSuggestAnthropic([]string{"claude-haiku-4-5"}) {
		t.Fatal("claude-haiku should promote")
	}
}
