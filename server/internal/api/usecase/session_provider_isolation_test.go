package usecase

import "testing"

func TestSessionProviderIsolationDisabled(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "opencode", "gemini", ""} {
		if SessionProviderIsolationAgent(agent) {
			t.Fatalf("session provider isolation must be disabled for %q", agent)
		}
	}
}
