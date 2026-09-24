package synthesizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/diff"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func applyChannelNameAttr(attrs map[string]string, name string) {
	if attrs == nil {
		return
	}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		attrs["channel_name"] = trimmed
	}
}

func applyChannelGroupAttr(attrs map[string]string, group, defaultPanel, baseURL string) {
	if attrs == nil {
		return
	}
	panel := resolveChannelPanel(defaultPanel, baseURL)
	if panel != "" {
		attrs[coreauth.AttributeChannelPanel] = panel
	}
	if trimmed := strings.TrimSpace(group); trimmed != "" {
		attrs[coreauth.AttributeChannelGroup] = trimmed
	}
}

const (
	kimiLegacyOpenAIBaseURL      = "https://api.moonshot.ai"
	kimiDomesticBaseURL          = "https://api.moonshot.cn"
	kimiOpenAIBaseURL            = "https://api.moonshot.ai/v1"
	kimiDomesticOpenAIBaseURL    = "https://api.moonshot.cn/v1"
	kimiAnthropicBaseURL         = "https://api.moonshot.ai/anthropic"
	kimiDomesticAnthropicBaseURL = "https://api.moonshot.cn/anthropic"
	lmuAIBaseURL                 = "https://api.lmuai.com"
	lmuAIOpenAIBaseURL           = "https://api.lmuai.com/v1"
)

func resolveChannelPanel(defaultPanel, baseURL string) string {
	panel := strings.TrimSpace(defaultPanel)
	normalized := normalizeChannelPanelBaseURL(baseURL)
	if normalized == "" {
		return panel
	}
	switch panel {
	case "codex":
		if isKimiCodexBaseURL(normalized) {
			return "kimi"
		}
		if isLmuAIOpenAIBaseURL(normalized) {
			return "lmu-ai"
		}
	case "claude":
		if isKimiClaudeBaseURL(normalized) {
			return "kimi"
		}
		if isLmuAIClaudeBaseURL(normalized) {
			return "lmu-ai"
		}
	case "openai-compatibility":
		if isKimiOpenAIBaseURL(normalized) {
			return "kimi"
		}
		if isLmuAIOpenAIBaseURL(normalized) {
			return "lmu-ai"
		}
	case "gemini":
		if isLmuAIGeminiBaseURL(normalized) {
			return "lmu-ai"
		}
	}
	return panel
}

func normalizeChannelPanelBaseURL(baseURL string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/")
}

func isKimiCodexBaseURL(normalized string) bool {
	return normalized == kimiOpenAIBaseURL || normalized == kimiDomesticOpenAIBaseURL
}

func isKimiClaudeBaseURL(normalized string) bool {
	return normalized == kimiAnthropicBaseURL || normalized == kimiDomesticAnthropicBaseURL
}

func isKimiOpenAIBaseURL(normalized string) bool {
	return isKimiCodexBaseURL(normalized) || normalized == kimiLegacyOpenAIBaseURL || normalized == kimiDomesticBaseURL
}

func isLmuAIOpenAIBaseURL(normalized string) bool {
	return normalized == lmuAIOpenAIBaseURL
}

func isLmuAIClaudeBaseURL(normalized string) bool {
	return normalized == lmuAIBaseURL
}

func isLmuAIGeminiBaseURL(normalized string) bool {
	return normalized == lmuAIBaseURL
}

// StableIDGenerator generates stable, deterministic IDs for auth entries.
// It uses SHA256 hashing with collision handling via counters.
// It is not safe for concurrent use.
type StableIDGenerator struct {
	counters map[string]int
}

// NewStableIDGenerator creates a new StableIDGenerator instance.
func NewStableIDGenerator() *StableIDGenerator {
	return &StableIDGenerator{counters: make(map[string]int)}
}

// Next generates a stable ID based on the kind and parts.
// Returns the full ID (kind:hash) and the short hash portion.
func (g *StableIDGenerator) Next(kind string, parts ...string) (string, string) {
	if g == nil {
		return kind + ":000000000000", "000000000000"
	}
	hasher := sha256.New()
	hasher.Write([]byte(kind))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		hasher.Write([]byte{0})
		hasher.Write([]byte(trimmed))
	}
	digest := hex.EncodeToString(hasher.Sum(nil))
	if len(digest) < 12 {
		digest = fmt.Sprintf("%012s", digest)
	}
	short := digest[:12]
	key := kind + ":" + short
	index := g.counters[key]
	g.counters[key] = index + 1
	if index > 0 {
		short = fmt.Sprintf("%s-%d", short, index)
	}
	return fmt.Sprintf("%s:%s", kind, short), short
}

// ApplyAuthExcludedModelsMeta applies excluded models metadata to an auth entry.
// It computes a hash of excluded models and sets the auth_kind attribute.
// Only per-key exclusions are applied; global oauth-excluded-models is ignored.
func ApplyAuthExcludedModelsMeta(auth *coreauth.Auth, cfg *config.Config, perKey []string, authKind string) {
	if auth == nil || cfg == nil {
		return
	}
	seen := make(map[string]struct{})
	add := func(list []string) {
		for _, entry := range list {
			if trimmed := strings.TrimSpace(entry); trimmed != "" {
				key := strings.ToLower(trimmed)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
			}
		}
	}
	add(perKey)
	combined := make([]string, 0, len(seen))
	for k := range seen {
		combined = append(combined, k)
	}
	sort.Strings(combined)
	hash := diff.ComputeExcludedModelsHash(combined)
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if hash != "" {
		auth.Attributes["excluded_models_hash"] = hash
	}
	// Store the combined excluded models list so that routing can read it at runtime
	if len(combined) > 0 {
		auth.Attributes["excluded_models"] = strings.Join(combined, ",")
	}
	if authKind != "" {
		auth.Attributes["auth_kind"] = authKind
	}
}

// addRequestRetryToMetadata copies a per-credential request-retry override into metadata.
// Nil or negative values are treated as unset and are not written.
func addRequestRetryToMetadata(requestRetry *int, metadata map[string]any) {
	if requestRetry == nil || *requestRetry < 0 || metadata == nil {
		return
	}
	metadata["request_retry"] = *requestRetry
}

// addStreamFirstTokenTimeoutToMetadata copies a positive per-channel first-token
// wait into metadata. Nil and non-positive values leave the feature disabled.
func addStreamFirstTokenTimeoutToMetadata(seconds *int, metadata map[string]any) {
	if seconds == nil || *seconds <= 0 || metadata == nil {
		return
	}
	metadata["stream_first_token_timeout_seconds"] = *seconds
}

// addMaxConcurrentConnectionsToMetadata copies a positive per-channel connection
// cap into metadata. Nil and non-positive values leave the channel unlimited.
func addMaxConcurrentConnectionsToMetadata(limit *int, metadata map[string]any) {
	if limit == nil || *limit <= 0 || metadata == nil {
		return
	}
	metadata["max_concurrent_connections"] = *limit
}

// addProviderRetryToMetadata copies same-credential retry settings into metadata.
// A nil count is omitted. A non-nil status-code slice is always written, including
// an empty list that disables status-code-triggered retries.
func addProviderRetryToMetadata(count *int, codes *[]int, metadata map[string]any) {
	if metadata == nil {
		return
	}
	if count != nil && *count >= 0 {
		metadata["provider_retry_count"] = *count
	}
	if codes != nil {
		metadata["provider_retry_status_codes"] = append([]int(nil), *codes...)
	}
}

// addRequestScopedErrorsToMetadata copies per-credential request-scoped error rules into metadata.
func addRequestScopedErrorsToMetadata(rules []config.RequestScopedErrorRule, metadata map[string]any) {
	if len(rules) == 0 || metadata == nil {
		return
	}
	metadata["request_scoped_errors"] = rules
}

// addHideNoAvailableChannelToMetadata copies the provider switch into metadata when enabled.
func addHideNoAvailableChannelToMetadata(hide bool, metadata map[string]any) {
	if !hide || metadata == nil {
		return
	}
	metadata["hide_no_available_channel"] = true
}

// addStreamFakeFirstTokensToMetadata copies sanitized Codex probe tokens into metadata.
func addStreamFakeFirstTokensToMetadata(tokens []string, metadata map[string]any) {
	if metadata == nil {
		return
	}
	sanitized := config.SanitizeStreamFakeFirstTokens(tokens)
	if len(sanitized) == 0 {
		return
	}
	metadata["stream_fake_first_tokens"] = sanitized
}

func fingerprintProfileFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	for _, key := range []string{"fingerprint_profile", "fingerprint-profile"} {
		raw, _ := metadata[key].(string)
		if profile := strings.ToLower(strings.TrimSpace(raw)); profile != "" {
			return profile
		}
	}
	return ""
}

// applyFingerprintProfileAttribute copies fingerprint-profile from an OAuth JSON
// file (Kimi, Claude, etc.) onto auth attributes so Claude Messages opt-in works
// the same way as claude-api-key config.
func applyFingerprintProfileAttribute(auth *coreauth.Auth, metadata map[string]any) {
	if auth == nil {
		return
	}
	profile := fingerprintProfileFromMetadata(metadata)
	if profile == "" {
		return
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["fingerprint_profile"] = profile
}

// addConfigHeadersToAttrs adds header configuration to auth attributes.
// Headers are prefixed with "header:" in the attributes map.
func addConfigHeadersToAttrs(headers map[string]string, attrs map[string]string) {
	if len(headers) == 0 || attrs == nil {
		return
	}
	for hk, hv := range headers {
		key := strings.TrimSpace(hk)
		val := strings.TrimSpace(hv)
		if key == "" || val == "" {
			continue
		}
		attrs["header:"+key] = val
	}
}
