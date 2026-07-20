package api

import (
	"encoding/json"
	"os"
	"path/filepath"

	"mindfs/server/internal/apperr"
)

// applyClaudeAPIProvider writes provider config to ~/.claude/settings.json.
func applyClaudeAPIProvider(provider agentAPIProvider) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return apperr.Wrap("mkdir", settingsPath, err)
	}

	settings := map[string]any{}
	if payload, err := os.ReadFile(settingsPath); err == nil && len(payload) > 0 {
		_ = json.Unmarshal(payload, &settings)
	}

	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}

	env["ANTHROPIC_AUTH_TOKEN"] = provider.APIKey
	env["ANTHROPIC_BASE_URL"] = provider.BaseURL

	// Set default model if available
	if len(provider.Models) > 0 {
		firstModel := provider.Models[0]
		env["ANTHROPIC_MODEL"] = firstModel

		// Try to intelligently set tier defaults
		for _, model := range provider.Models {
			lower := toLower(model)
			if contains(lower, "opus") {
				env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = model
			}
			if contains(lower, "sonnet") {
				env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = model
			}
			if contains(lower, "haiku") {
				env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
			}
		}
	}

	settings["env"] = env

	payload, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return apperr.Wrap("write", settingsPath, os.WriteFile(settingsPath, append(payload, '\n'), 0o600))
}

func toLower(s string) string {
	// Simple ASCII lowercase
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
