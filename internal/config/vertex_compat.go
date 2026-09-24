package config

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// VertexCompatKey represents the configuration for Vertex AI-compatible API keys.
// This supports third-party services that use Vertex AI-style endpoint paths
// (/publishers/google/models/{model}:streamGenerateContent) but authenticate
// with simple API keys instead of Google Cloud service account credentials.
//
// Example services: zenmux.ai and similar Vertex-compatible providers.
type VertexCompatKey struct {
	// APIKey is the authentication key for accessing the Vertex-compatible API.
	// Maps to the x-goog-api-key header. Optional when ServiceAccount is set.
	APIKey string `yaml:"api-key" json:"api-key"`

	// Name is the unique channel identity within vertex-api-key.
	// An empty name is a legacy channel and is not required to be unique.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Group assigns this channel to one custom group. Empty means ungrouped.
	Group string `yaml:"group,omitempty" json:"group,omitempty"`

	// ServiceAccount is optional official Google Cloud service-account JSON.
	// When present, the Vertex executor uses ADC-style service account auth.
	ServiceAccount map[string]any `yaml:"service-account,omitempty" json:"service-account,omitempty"`

	// ProjectID optionally overrides the service-account project_id.
	ProjectID string `yaml:"project-id,omitempty" json:"project-id,omitempty"`

	// Location optionally sets a default Vertex region (e.g. us-central1).
	Location string `yaml:"location,omitempty" json:"location,omitempty"`

	// Email is an optional display identity for service-account credentials.
	Email string `yaml:"email,omitempty" json:"email,omitempty"`

	// Priority controls selection preference when multiple credentials match.
	// Higher values are preferred; defaults to 0.
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// Weight controls proportional selection under weighted-round-robin.
	// An omitted value defaults to 1; non-positive values exclude this credential; maximum 1,000,000.
	Weight *int `yaml:"weight,omitempty" json:"weight,omitempty"`

	// Prefix optionally namespaces model aliases for this credential (e.g., "teamA/vertex-pro").
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`

	// BaseURL optionally overrides the Vertex-compatible API endpoint.
	// The executor will append "/v1/publishers/google/models/{model}:action" to this.
	// When empty, requests fall back to the default Vertex API base URL.
	BaseURL string `yaml:"base-url,omitempty" json:"base-url,omitempty"`

	// ProxyURL optionally overrides the global proxy for this API key.
	ProxyURL string `yaml:"proxy-url,omitempty" json:"proxy-url,omitempty"`

	// Headers optionally adds extra HTTP headers for requests sent with this key.
	// Commonly used for cookies, user-agent, and other authentication headers.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	// Models defines the model configurations including aliases for routing.
	Models []VertexCompatModel `yaml:"models,omitempty" json:"models,omitempty"`

	// ExcludedModels lists model IDs that should be excluded for this provider.
	ExcludedModels []string `yaml:"excluded-models,omitempty" json:"excluded-models,omitempty"`

	// DisableCooling overrides the global cooling policy for this credential when set.
	// True disables auth/model cooldowns; false explicitly enables them.
	DisableCooling *bool `yaml:"disable-cooling,omitempty" json:"disable-cooling,omitempty"`

	// RequestRetry optionally overrides the global request-retry for this credential.
	// Nil or a negative value means "use the global request-retry". 0 disables additional retry rounds.
	RequestRetry *int `yaml:"request-retry,omitempty" json:"request-retry,omitempty"`

	// StreamFirstTokenTimeoutSeconds cancels a streaming attempt and fails over to
	// the next channel when no first token arrives within this many seconds.
	// Nil or 0 leaves the wait disabled. Values must be between 0 and 3600.
	StreamFirstTokenTimeoutSeconds *int `yaml:"stream-first-token-timeout-seconds,omitempty" json:"stream-first-token-timeout-seconds,omitempty"`

	// MaxConcurrentConnections limits how many requests may occupy this channel at once.
	// Nil or 0 means unlimited. Values must be between 0 and 1,000,000.
	MaxConcurrentConnections *int `yaml:"max-concurrent-connections,omitempty" json:"max-concurrent-connections,omitempty"`

	// ProviderRetryCount is the number of same-credential retries before failover.
	// Nil or a negative value disables same-credential retry. Values above 10 are clamped to 10.
	ProviderRetryCount *int `yaml:"provider-retry-count,omitempty" json:"provider-retry-count,omitempty"`

	// ProviderRetryStatusCodes lists HTTP statuses that trigger same-credential retry.
	// Nil uses the default list (401, 403, 429) when ProviderRetryCount is positive.
	// A non-nil empty slice disables status-code-triggered same-credential retry.
	ProviderRetryStatusCodes *[]int `yaml:"provider-retry-status-codes,omitempty" json:"provider-retry-status-codes,omitempty"`

	// HideNoAvailableChannel replaces downstream 503 "No available channel for model"
	// payloads with a generic Service Unavailable envelope. Management connectivity
	// tests still see the original upstream body.
	HideNoAvailableChannel bool `yaml:"hide-no-available-channel,omitempty" json:"hide-no-available-channel,omitempty"`
}

func (k VertexCompatKey) GetAPIKey() string   { return k.APIKey }
func (k VertexCompatKey) GetBaseURL() string  { return k.BaseURL }
func (k VertexCompatKey) GetPrefix() string   { return k.Prefix }
func (k VertexCompatKey) GetProxyURL() string { return k.ProxyURL }

// VertexCompatModel represents a model configuration for Vertex compatibility,
// including the actual model name and its alias for API routing.
type VertexCompatModel struct {
	// Name is the actual model name used by the external provider.
	Name string `yaml:"name" json:"name"`

	// Alias is the model name alias that clients will use to reference this model.
	Alias string `yaml:"alias" json:"alias"`

	// DisplayName is the optional human-readable name shown in model catalogs.
	DisplayName string `yaml:"display-name,omitempty" json:"display-name,omitempty"`

	// ForceMapping rewrites upstream response model fields back to Alias.
	ForceMapping bool `yaml:"force-mapping,omitempty" json:"force-mapping,omitempty"`

	// Thinking configures the thinking/reasoning capability for this model.
	Thinking *registry.ThinkingSupport `yaml:"thinking,omitempty" json:"thinking,omitempty"`
}

func (m VertexCompatModel) GetName() string        { return m.Name }
func (m VertexCompatModel) GetAlias() string       { return m.Alias }
func (m VertexCompatModel) GetDisplayName() string { return m.DisplayName }
func (m VertexCompatModel) GetForceMapping() bool  { return m.ForceMapping }
func (m VertexCompatModel) GetThinking() *registry.ThinkingSupport {
	return m.Thinking
}

// SanitizeVertexCompatKeys normalizes Vertex-compatible API key credentials.
// Unnamed entries that share an API key, base URL, and service account are collapsed.
// Entries that share a base URL and API key stay distinct when their names differ.
func (cfg *Config) SanitizeVertexCompatKeys() {
	if cfg == nil {
		return
	}

	seenUnnamed := make(map[string]struct{}, len(cfg.VertexCompatAPIKey))
	out := cfg.VertexCompatAPIKey[:0]
	for i := range cfg.VertexCompatAPIKey {
		entry := cfg.VertexCompatAPIKey[i]
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		normalizeChannelIdentity(&entry.Name, &entry.Group)
		entry.ProjectID = strings.TrimSpace(entry.ProjectID)
		entry.Location = strings.TrimSpace(entry.Location)
		entry.Email = strings.TrimSpace(entry.Email)
		entry.ServiceAccount = NormalizeServiceAccount(entry.ServiceAccount)
		if entry.APIKey == "" && len(entry.ServiceAccount) == 0 {
			continue
		}
		entry.Prefix = normalizeModelPrefix(entry.Prefix)
		entry.BaseURL = strings.TrimSpace(entry.BaseURL)
		entry.ProxyURL = strings.TrimSpace(entry.ProxyURL)
		entry.Headers = NormalizeHeaders(entry.Headers)
		entry.ExcludedModels = NormalizeExcludedModels(entry.ExcludedModels)
		sanitizeProviderRetryFields(&entry.ProviderRetryCount, &entry.ProviderRetryStatusCodes)

		// Sanitize models: remove entries without valid alias
		sanitizedModels := make([]VertexCompatModel, 0, len(entry.Models))
		for _, model := range entry.Models {
			model.Alias = strings.TrimSpace(model.Alias)
			model.Name = strings.TrimSpace(model.Name)
			if model.Alias != "" && model.Name != "" {
				sanitizedModels = append(sanitizedModels, model)
			}
		}
		entry.Models = sanitizedModels
		if entry.Name == "" {
			uniqueKey := entry.APIKey + "|" + entry.BaseURL + "|" + ServiceAccountIdentity(entry.ServiceAccount)
			if _, exists := seenUnnamed[uniqueKey]; exists {
				continue
			}
			seenUnnamed[uniqueKey] = struct{}{}
		}
		out = append(out, entry)
	}
	cfg.VertexCompatAPIKey = out
}

// NormalizeServiceAccount drops empty service-account maps.
func NormalizeServiceAccount(account map[string]any) map[string]any {
	if len(account) == 0 {
		return nil
	}
	return account
}

// ServiceAccountIdentity returns a stable identity for a service-account payload.
func ServiceAccountIdentity(account map[string]any) string {
	if len(account) == 0 {
		return ""
	}
	for _, key := range []string{"client_email", "client_id", "project_id"} {
		if value, ok := account[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return "service-account"
}
