package config

import (
	"fmt"
	"sort"
	"strings"
)

// NormalizeChannelName trims a channel name. An empty result is a legacy ungrouped-identity channel.
func NormalizeChannelName(name string) string {
	return strings.TrimSpace(name)
}

// NormalizeChannelGroup trims a channel group. An empty result means ungrouped.
func NormalizeChannelGroup(group string) string {
	return strings.TrimSpace(group)
}

// APIKeyChannelIDParts is the stable auth ID material shared by Gemini, Interactions,
// Claude, Codex, and xAI. Empty names are omitted.
func APIKeyChannelIDParts(apiKey, baseURL, proxyURL, prefix, name string, headers map[string]string) []string {
	return AppendChannelNameIDPart([]string{
		strings.TrimSpace(apiKey),
		strings.TrimSpace(baseURL),
		strings.TrimSpace(proxyURL),
		strings.TrimSpace(prefix),
		FormatSortedHeaders(headers),
	}, name)
}

// AntigravityChannelIDParts is the stable auth ID material for Antigravity API keys.
func AntigravityChannelIDParts(apiKey, baseURL, proxyURL, prefix, projectID, name string, headers map[string]string) []string {
	return AppendChannelNameIDPart([]string{
		strings.TrimSpace(apiKey),
		strings.TrimSpace(baseURL),
		strings.TrimSpace(proxyURL),
		strings.TrimSpace(prefix),
		strings.TrimSpace(projectID),
		FormatSortedHeaders(headers),
	}, name)
}

// VertexChannelIDParts is the stable auth ID material for Vertex-compatible credentials.
func VertexChannelIDParts(apiKey, baseURL, proxyURL, name string, serviceAccount map[string]any) []string {
	return AppendChannelNameIDPart([]string{
		strings.TrimSpace(apiKey),
		strings.TrimSpace(baseURL),
		strings.TrimSpace(proxyURL),
		ServiceAccountIdentity(serviceAccount),
	}, name)
}

// AppendChannelNameIDPart adds a non-empty channel name to stable auth ID parts.
// Empty names are omitted so legacy credentials keep their existing IDs.
func AppendChannelNameIDPart(parts []string, name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return parts
	}
	out := make([]string, len(parts)+1)
	copy(out, parts)
	out[len(parts)] = trimmed
	return out
}

// UsageCompositeKey builds the management usage map key.
// Named channels include the name so identical base URL and API key stay distinct.
// Unnamed legacy channels keep the historical "base|apiKey" key.
func UsageCompositeKey(baseURL, apiKey, channelName string) string {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	channelName = strings.TrimSpace(channelName)
	legacy := baseURL + "|" + apiKey
	if channelName == "" {
		return legacy
	}
	return channelName + "\x00" + legacy
}

func normalizeChannelIdentity(name, group *string) {
	if name != nil {
		*name = NormalizeChannelName(*name)
	}
	if group != nil {
		*group = NormalizeChannelGroup(*group)
	}
}

// NormalizeChannelGroups trims group catalog entries, drops blanks, and
// deduplicates names within each provider. An absent catalog leaves every
// channel ungrouped.
func (cfg *Config) NormalizeChannelGroups() {
	if cfg == nil || len(cfg.ChannelGroups) == 0 {
		if cfg != nil {
			cfg.ChannelGroups = nil
		}
		return
	}
	keys := make([]string, 0, len(cfg.ChannelGroups))
	for rawKey := range cfg.ChannelGroups {
		keys = append(keys, rawKey)
	}
	sort.Strings(keys)
	out := make(map[string][]string, len(keys))
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		seen := make(map[string]struct{})
		cleaned := make([]string, 0, len(cfg.ChannelGroups[rawKey]))
		appendUnique := func(groups []string) {
			for _, group := range groups {
				name := strings.TrimSpace(group)
				if name == "" {
					continue
				}
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				cleaned = append(cleaned, name)
			}
		}
		appendUnique(out[key])
		appendUnique(cfg.ChannelGroups[rawKey])
		if len(cleaned) == 0 {
			continue
		}
		out[key] = cleaned
	}
	if len(out) == 0 {
		cfg.ChannelGroups = nil
		return
	}
	cfg.ChannelGroups = out
}

// ValidateUniqueChannelNames rejects repeated non-empty names in one provider list.
// Empty names are legacy channels and may repeat.
// openai-compatibility names are compared case-insensitively because that provider key is lowercased.
func ValidateUniqueChannelNames(section string, names []string) error {
	ignoreCase := section == "openai-compatibility"
	seen := make(map[string]int, len(names))
	for index, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		key := trimmed
		if ignoreCase {
			key = strings.ToLower(trimmed)
		}
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("%s[%d].name duplicates %s[%d].name %q", section, index, section, previous, trimmed)
		}
		seen[key] = index
	}
	return nil
}

// ValidateChannelNames checks every provider channel list for duplicate names.
func (cfg *Config) ValidateChannelNames() error {
	if cfg == nil {
		return nil
	}
	geminiNames := make([]string, len(cfg.GeminiKey))
	for i := range cfg.GeminiKey {
		geminiNames[i] = cfg.GeminiKey[i].Name
	}
	if err := ValidateUniqueChannelNames("gemini-api-key", geminiNames); err != nil {
		return err
	}
	interactionNames := make([]string, len(cfg.InteractionsKey))
	for i := range cfg.InteractionsKey {
		interactionNames[i] = cfg.InteractionsKey[i].Name
	}
	if err := ValidateUniqueChannelNames("interactions-api-key", interactionNames); err != nil {
		return err
	}
	claudeNames := make([]string, len(cfg.ClaudeKey))
	for i := range cfg.ClaudeKey {
		claudeNames[i] = cfg.ClaudeKey[i].Name
	}
	if err := ValidateUniqueChannelNames("claude-api-key", claudeNames); err != nil {
		return err
	}
	codexNames := make([]string, len(cfg.CodexKey))
	for i := range cfg.CodexKey {
		codexNames[i] = cfg.CodexKey[i].Name
	}
	if err := ValidateUniqueChannelNames("codex-api-key", codexNames); err != nil {
		return err
	}
	xaiNames := make([]string, len(cfg.XAIKey))
	for i := range cfg.XAIKey {
		xaiNames[i] = cfg.XAIKey[i].Name
	}
	if err := ValidateUniqueChannelNames("xai-api-key", xaiNames); err != nil {
		return err
	}
	vertexNames := make([]string, len(cfg.VertexCompatAPIKey))
	for i := range cfg.VertexCompatAPIKey {
		vertexNames[i] = cfg.VertexCompatAPIKey[i].Name
	}
	if err := ValidateUniqueChannelNames("vertex-api-key", vertexNames); err != nil {
		return err
	}
	antigravityNames := make([]string, len(cfg.AntigravityKey))
	for i := range cfg.AntigravityKey {
		antigravityNames[i] = cfg.AntigravityKey[i].Name
	}
	if err := ValidateUniqueChannelNames("antigravity-api-key", antigravityNames); err != nil {
		return err
	}
	openAINames := make([]string, len(cfg.OpenAICompatibility))
	for i := range cfg.OpenAICompatibility {
		openAINames[i] = cfg.OpenAICompatibility[i].Name
	}
	return ValidateUniqueChannelNames("openai-compatibility", openAINames)
}
