package config

import "fmt"

// MaxConcurrentConnectionsLimit is the upper bound for a channel's in-flight
// request cap. Zero means the channel accepts an unlimited number of connections.
const MaxConcurrentConnectionsLimit = 1000000

// ValidateMaxConcurrentConnections accepts a missing value and 0 through
// MaxConcurrentConnectionsLimit. Negative values and values above the maximum
// are rejected. Zero and nil mean unlimited.
func ValidateMaxConcurrentConnections(limit *int) error {
	if limit == nil {
		return nil
	}
	if *limit < 0 || *limit > MaxConcurrentConnectionsLimit {
		return fmt.Errorf("max-concurrent-connections must be between 0 and %d", MaxConcurrentConnectionsLimit)
	}
	return nil
}

// ValidateMaxConcurrentConnectionLimits checks every channel that can carry the setting.
func (cfg *Config) ValidateMaxConcurrentConnectionLimits() error {
	if cfg == nil {
		return nil
	}
	check := func(label string, limit *int) error {
		if err := ValidateMaxConcurrentConnections(limit); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	for i := range cfg.GeminiKey {
		if err := check(fmt.Sprintf("gemini-api-key[%d].max-concurrent-connections", i), cfg.GeminiKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.InteractionsKey {
		if err := check(fmt.Sprintf("interactions-api-key[%d].max-concurrent-connections", i), cfg.InteractionsKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.ClaudeKey {
		if err := check(fmt.Sprintf("claude-api-key[%d].max-concurrent-connections", i), cfg.ClaudeKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.CodexKey {
		if err := check(fmt.Sprintf("codex-api-key[%d].max-concurrent-connections", i), cfg.CodexKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.XAIKey {
		if err := check(fmt.Sprintf("xai-api-key[%d].max-concurrent-connections", i), cfg.XAIKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.OpenAICompatibility {
		if err := check(fmt.Sprintf("openai-compatibility[%d].max-concurrent-connections", i), cfg.OpenAICompatibility[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.VertexCompatAPIKey {
		if err := check(fmt.Sprintf("vertex-api-key[%d].max-concurrent-connections", i), cfg.VertexCompatAPIKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	for i := range cfg.AntigravityKey {
		if err := check(fmt.Sprintf("antigravity-api-key[%d].max-concurrent-connections", i), cfg.AntigravityKey[i].MaxConcurrentConnections); err != nil {
			return err
		}
	}
	return nil
}
