package config

import "fmt"

// MaxStreamFirstTokenTimeoutSeconds is the upper bound for a channel's
// streaming first-token wait. Zero disables the wait.
const MaxStreamFirstTokenTimeoutSeconds = 3600

// ValidateStreamFirstTokenTimeout accepts a missing value and 0 through
// MaxStreamFirstTokenTimeoutSeconds. Negative values and values above the
// maximum are rejected.
func ValidateStreamFirstTokenTimeout(seconds *int) error {
	if seconds == nil {
		return nil
	}
	if *seconds < 0 || *seconds > MaxStreamFirstTokenTimeoutSeconds {
		return fmt.Errorf("stream-first-token-timeout-seconds must be between 0 and %d", MaxStreamFirstTokenTimeoutSeconds)
	}
	return nil
}

// ValidateStreamFirstTokenTimeouts checks every channel that can carry the setting.
func (cfg *Config) ValidateStreamFirstTokenTimeouts() error {
	if cfg == nil {
		return nil
	}
	check := func(label string, seconds *int) error {
		if err := ValidateStreamFirstTokenTimeout(seconds); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	for i := range cfg.GeminiKey {
		if err := check(fmt.Sprintf("gemini-api-key[%d].stream-first-token-timeout-seconds", i), cfg.GeminiKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.InteractionsKey {
		if err := check(fmt.Sprintf("interactions-api-key[%d].stream-first-token-timeout-seconds", i), cfg.InteractionsKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.ClaudeKey {
		if err := check(fmt.Sprintf("claude-api-key[%d].stream-first-token-timeout-seconds", i), cfg.ClaudeKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.CodexKey {
		if err := check(fmt.Sprintf("codex-api-key[%d].stream-first-token-timeout-seconds", i), cfg.CodexKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.XAIKey {
		if err := check(fmt.Sprintf("xai-api-key[%d].stream-first-token-timeout-seconds", i), cfg.XAIKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.OpenAICompatibility {
		if err := check(fmt.Sprintf("openai-compatibility[%d].stream-first-token-timeout-seconds", i), cfg.OpenAICompatibility[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.VertexCompatAPIKey {
		if err := check(fmt.Sprintf("vertex-api-key[%d].stream-first-token-timeout-seconds", i), cfg.VertexCompatAPIKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	for i := range cfg.AntigravityKey {
		if err := check(fmt.Sprintf("antigravity-api-key[%d].stream-first-token-timeout-seconds", i), cfg.AntigravityKey[i].StreamFirstTokenTimeoutSeconds); err != nil {
			return err
		}
	}
	return nil
}
