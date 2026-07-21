package preferences

import "testing"

func TestClearAgentLastConfigSelectionPreservesDefaults(t *testing.T) {
	store := &Store{
		path: t.TempDir() + "/preferences.json",
		data: UserPreferences{Agents: map[string]AgentDefaults{
			"codex": {
				Model: "gpt-5",
				LastConfigSelection: &LastConfigSelection{
					Type: "api_provider",
					ID:   "api-work",
				},
			},
		}},
	}

	if err := store.ClearAgentLastConfigSelection("codex"); err != nil {
		t.Fatal(err)
	}
	got := store.data.Agents["codex"]
	if got.LastConfigSelection != nil {
		t.Fatalf("selection = %#v, want nil", got.LastConfigSelection)
	}
	if got.Model != "gpt-5" {
		t.Fatalf("model = %q, want gpt-5", got.Model)
	}
}
