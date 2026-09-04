package diff

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestBuildConfigChangeDetailsIncludesAllCoolingOverrides(t *testing.T) {
	disabled := true
	enabled := false
	tests := []struct {
		name   string
		oldCfg *config.Config
		newCfg *config.Config
		want   string
	}{
		{
			name:   "gemini inherit to false",
			oldCfg: &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "gemini-key"}}},
			newCfg: &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "gemini-key", DisableCooling: &enabled}}},
			want:   "gemini[0].disable-cooling: inherit -> false",
		},
		{
			name:   "interactions false to true",
			oldCfg: &config.Config{InteractionsKey: []config.GeminiKey{{APIKey: "interactions-key", DisableCooling: &enabled}}},
			newCfg: &config.Config{InteractionsKey: []config.GeminiKey{{APIKey: "interactions-key", DisableCooling: &disabled}}},
			want:   "interactions[0].disable-cooling: false -> true",
		},
		{
			name:   "claude false to true",
			oldCfg: &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "claude-key", DisableCooling: &enabled}}},
			newCfg: &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "claude-key", DisableCooling: &disabled}}},
			want:   "claude[0].disable-cooling: false -> true",
		},
		{
			name:   "codex true to inherit",
			oldCfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "codex-key", DisableCooling: &disabled}}},
			newCfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "codex-key"}}},
			want:   "codex[0].disable-cooling: true -> inherit",
		},
		{
			name:   "xai inherit to true",
			oldCfg: &config.Config{XAIKey: []config.XAIKey{{APIKey: "xai-key"}}},
			newCfg: &config.Config{XAIKey: []config.XAIKey{{APIKey: "xai-key", DisableCooling: &disabled}}},
			want:   "xai[0].disable-cooling: inherit -> true",
		},
		{
			name: "openai compatibility false to inherit",
			oldCfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
				Name: "compat", BaseURL: "https://compat.example.com", DisableCooling: &enabled,
			}}},
			newCfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
				Name: "compat", BaseURL: "https://compat.example.com",
			}}},
			want: "disable-cooling false -> inherit",
		},
		{
			name:   "vertex inherit to false",
			oldCfg: &config.Config{VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "vertex-key"}}},
			newCfg: &config.Config{VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "vertex-key", DisableCooling: &enabled}}},
			want:   "vertex[0].disable-cooling: inherit -> false",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changes := strings.Join(BuildConfigChangeDetails(tc.oldCfg, tc.newCfg), "\n")
			if !strings.Contains(changes, tc.want) {
				t.Fatalf("changes missing %q:\n%s", tc.want, changes)
			}
		})
	}
}

func TestBuildConfigChangeDetailsIncludesHideNoAvailableChannel(t *testing.T) {
	tests := []struct {
		name   string
		oldCfg *config.Config
		newCfg *config.Config
		want   string
	}{
		{
			name:   "gemini",
			oldCfg: &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "gemini-key"}}},
			newCfg: &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "gemini-key", HideNoAvailableChannel: true}}},
			want:   "gemini[0].hide-no-available-channel: false -> true",
		},
		{
			name:   "claude",
			oldCfg: &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "claude-key"}}},
			newCfg: &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "claude-key", HideNoAvailableChannel: true}}},
			want:   "claude[0].hide-no-available-channel: false -> true",
		},
		{
			name:   "codex",
			oldCfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "codex-key"}}},
			newCfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "codex-key", HideNoAvailableChannel: true}}},
			want:   "codex[0].hide-no-available-channel: false -> true",
		},
		{
			name:   "xai",
			oldCfg: &config.Config{XAIKey: []config.XAIKey{{APIKey: "xai-key"}}},
			newCfg: &config.Config{XAIKey: []config.XAIKey{{APIKey: "xai-key", HideNoAvailableChannel: true}}},
			want:   "xai[0].hide-no-available-channel: false -> true",
		},
		{
			name:   "vertex",
			oldCfg: &config.Config{VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "vertex-key"}}},
			newCfg: &config.Config{VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "vertex-key", HideNoAvailableChannel: true}}},
			want:   "vertex[0].hide-no-available-channel: false -> true",
		},
		{
			name: "openai compatibility",
			oldCfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
				Name: "compat", BaseURL: "https://compat.example.com",
			}}},
			newCfg: &config.Config{OpenAICompatibility: []config.OpenAICompatibility{{
				Name: "compat", BaseURL: "https://compat.example.com", HideNoAvailableChannel: true,
			}}},
			want: "hide-no-available-channel false -> true",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			changes := strings.Join(BuildConfigChangeDetails(tc.oldCfg, tc.newCfg), "\n")
			if !strings.Contains(changes, tc.want) {
				t.Fatalf("changes missing %q:\n%s", tc.want, changes)
			}
		})
	}
}
