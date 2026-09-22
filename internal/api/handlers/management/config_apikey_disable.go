package management

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const configAPIKeyDisablePattern = "*"

func setConfigAPIKeyExcludedAll(models []string, disable bool) []string {
	if disable {
		for _, item := range models {
			if strings.TrimSpace(item) == configAPIKeyDisablePattern {
				return config.NormalizeExcludedModels(models)
			}
		}
		return config.NormalizeExcludedModels(append(append([]string(nil), models...), configAPIKeyDisablePattern))
	}
	filtered := make([]string, 0, len(models))
	for _, item := range models {
		if strings.TrimSpace(item) == configAPIKeyDisablePattern {
			continue
		}
		filtered = append(filtered, item)
	}
	return config.NormalizeExcludedModels(filtered)
}

func toggleConfigAPIKeyExcludedAll(cfg *config.Config, auth *coreauth.Auth, disable bool) (bool, error) {
	if cfg == nil || auth == nil || !coreauth.IsConfigAPIKeyAuth(auth) {
		return false, nil
	}
	authID := strings.TrimSpace(auth.ID)
	if authID == "" {
		return false, fmt.Errorf("auth id is empty")
	}

	idGen := synthesizer.NewStableIDGenerator()

	for i := range cfg.GeminiKey {
		entry := &cfg.GeminiKey[i]
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id, _ := idGen.Next("gemini:apikey", config.APIKeyChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Name, entry.Headers)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}
	for i := range cfg.InteractionsKey {
		entry := &cfg.InteractionsKey[i]
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id, _ := idGen.Next("gemini-interactions:apikey", config.APIKeyChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Name, entry.Headers)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}
	for i := range cfg.ClaudeKey {
		entry := &cfg.ClaudeKey[i]
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id, _ := idGen.Next("claude:apikey", config.APIKeyChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Name, entry.Headers)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}
	for i := range cfg.CodexKey {
		entry := &cfg.CodexKey[i]
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id, _ := idGen.Next("codex:apikey", config.APIKeyChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Name, entry.Headers)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}
	for i := range cfg.XAIKey {
		entry := &cfg.XAIKey[i]
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id, _ := idGen.Next("xai:apikey", config.APIKeyChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, entry.Name, entry.Headers)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}
	for i := range cfg.VertexCompatAPIKey {
		entry := &cfg.VertexCompatAPIKey[i]
		id, _ := idGen.Next("vertex:apikey", config.VertexChannelIDParts(entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Name, entry.ServiceAccount)...)
		if id == authID {
			entry.ExcludedModels = setConfigAPIKeyExcludedAll(entry.ExcludedModels, disable)
			return true, nil
		}
	}

	return false, nil
}
