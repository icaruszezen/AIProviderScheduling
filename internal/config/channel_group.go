package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxChannelRetryCount is the upper bound for a group's cross-channel switches.
const MaxChannelRetryCount = 100

// ChannelGroup is one named group inside a provider panel.
// YAML and JSON accept either a bare name or this object.
type ChannelGroup struct {
	Name                      string   `yaml:"name" json:"name"`
	APIKeys                   []string `yaml:"api-keys,omitempty" json:"api-keys,omitempty"`
	ChannelRetryCount         *int     `yaml:"channel-retry-count,omitempty" json:"channel-retry-count,omitempty"`
	ChannelRetryStatusCodes   []int    `yaml:"channel-retry-status-codes,omitempty" json:"channel-retry-status-codes,omitempty"`
	ChannelRetryErrorContains []string `yaml:"channel-retry-error-contains,omitempty" json:"channel-retry-error-contains,omitempty"`
}

// ChannelGroupCredential is one client API key bound to a single panel group.
type ChannelGroupCredential struct {
	Panel      string
	Group      string
	PolicyJSON string
}

type channelGroupPolicyDocument struct {
	Count         int      `json:"count"`
	StatusCodes   []int    `json:"status-codes,omitempty"`
	ErrorContains []string `json:"error-contains,omitempty"`
}

// UnmarshalYAML accepts a group name or a group object.
func (g *ChannelGroup) UnmarshalYAML(value *yaml.Node) error {
	if g == nil {
		return fmt.Errorf("channel group is nil")
	}
	if value != nil && value.Kind == yaml.ScalarNode {
		g.Name = value.Value
		return nil
	}
	type plain ChannelGroup
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*g = ChannelGroup(decoded)
	return nil
}

// UnmarshalJSON accepts a group name or a group object.
func (g *ChannelGroup) UnmarshalJSON(data []byte) error {
	if g == nil {
		return fmt.Errorf("channel group is nil")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var name string
		if err := json.Unmarshal(trimmed, &name); err != nil {
			return err
		}
		*g = ChannelGroup{Name: name}
		return nil
	}
	type plain ChannelGroup
	var decoded plain
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return err
	}
	*g = ChannelGroup(decoded)
	return nil
}

// NormalizeChannelGroups trims catalog entries, drops blank names, and rejects
// invalid group API keys, retry rules, or duplicate names in one panel.
// An absent catalog leaves every channel ungrouped.
func (cfg *Config) NormalizeChannelGroups() error {
	if cfg == nil || len(cfg.ChannelGroups) == 0 {
		if cfg != nil {
			cfg.ChannelGroups = nil
		}
		return nil
	}
	globalKeys := make(map[string]struct{}, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		globalKeys[trimmed] = struct{}{}
	}
	usedKeys := make(map[string]struct{})
	rawKeys := make([]string, 0, len(cfg.ChannelGroups))
	for rawKey := range cfg.ChannelGroups {
		rawKeys = append(rawKeys, rawKey)
	}
	sort.Strings(rawKeys)
	merged := make(map[string][]ChannelGroup, len(rawKeys))
	panels := make([]string, 0, len(rawKeys))
	for _, rawKey := range rawKeys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		if _, exists := merged[key]; !exists {
			panels = append(panels, key)
		}
		merged[key] = append(merged[key], cfg.ChannelGroups[rawKey]...)
	}
	out := make(map[string][]ChannelGroup, len(panels))
	for _, key := range panels {
		seen := make(map[string]struct{})
		cleaned := make([]ChannelGroup, 0, len(merged[key]))
		for _, group := range merged[key] {
			name := strings.TrimSpace(group.Name)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				return fmt.Errorf("channel group %q is duplicated in panel %q", name, key)
			}
			normalized, errNormalize := normalizeChannelGroup(group, globalKeys, usedKeys)
			if errNormalize != nil {
				return errNormalize
			}
			if normalized.Name == "" {
				continue
			}
			seen[normalized.Name] = struct{}{}
			cleaned = append(cleaned, normalized)
		}
		if len(cleaned) == 0 {
			continue
		}
		out[key] = cleaned
	}
	if len(out) == 0 {
		cfg.ChannelGroups = nil
		return nil
	}
	cfg.ChannelGroups = out
	return nil
}

func normalizeChannelGroup(group ChannelGroup, globalKeys, usedKeys map[string]struct{}) (ChannelGroup, error) {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" {
		return ChannelGroup{}, nil
	}
	keys, errKeys := normalizeChannelGroupAPIKeys(group.APIKeys, globalKeys, usedKeys)
	if errKeys != nil {
		return ChannelGroup{}, errKeys
	}
	group.APIKeys = keys
	if group.ChannelRetryCount != nil {
		count := *group.ChannelRetryCount
		if count < 0 || count > MaxChannelRetryCount {
			return ChannelGroup{}, fmt.Errorf("channel-retry-count must be between 0 and %d", MaxChannelRetryCount)
		}
		copied := count
		group.ChannelRetryCount = &copied
	}
	codes, errCodes := normalizeChannelRetryStatusCodes(group.ChannelRetryStatusCodes)
	if errCodes != nil {
		return ChannelGroup{}, errCodes
	}
	group.ChannelRetryStatusCodes = codes
	group.ChannelRetryErrorContains = normalizeChannelRetryErrorContains(group.ChannelRetryErrorContains)
	return group, nil
}

func normalizeChannelGroupAPIKeys(keys []string, globalKeys, usedKeys map[string]struct{}) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return nil, fmt.Errorf("channel group api key is empty")
		}
		if _, exists := seen[trimmed]; exists {
			return nil, fmt.Errorf("channel group api key is duplicated")
		}
		if _, exists := globalKeys[trimmed]; exists {
			return nil, fmt.Errorf("channel group api key duplicates a global api-keys entry")
		}
		if _, exists := usedKeys[trimmed]; exists {
			return nil, fmt.Errorf("channel group api key is duplicated")
		}
		seen[trimmed] = struct{}{}
		usedKeys[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

func normalizeChannelRetryStatusCodes(codes []int) ([]int, error) {
	if len(codes) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(codes))
	seen := make(map[int]struct{}, len(codes))
	for _, code := range codes {
		if code < 100 || code > 599 {
			return nil, fmt.Errorf("channel-retry-status-codes must be between 100 and 599")
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out, nil
}

func normalizeChannelRetryErrorContains(phrases []string) []string {
	if len(phrases) == 0 {
		return nil
	}
	out := make([]string, 0, len(phrases))
	seen := make(map[string]struct{}, len(phrases))
	for _, phrase := range phrases {
		trimmed := strings.TrimSpace(phrase)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ChannelGroupCredentials indexes client API keys that are bound to one panel group.
func (cfg *Config) ChannelGroupCredentials() map[string]ChannelGroupCredential {
	if cfg == nil || len(cfg.ChannelGroups) == 0 {
		return nil
	}
	out := make(map[string]ChannelGroupCredential)
	panels := make([]string, 0, len(cfg.ChannelGroups))
	for panel := range cfg.ChannelGroups {
		panels = append(panels, panel)
	}
	sort.Strings(panels)
	for _, panel := range panels {
		for _, group := range cfg.ChannelGroups[panel] {
			if strings.TrimSpace(group.Name) == "" || len(group.APIKeys) == 0 {
				continue
			}
			policy := group.accessPolicyJSON()
			for _, key := range group.APIKeys {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					continue
				}
				out[trimmed] = ChannelGroupCredential{
					Panel:      panel,
					Group:      group.Name,
					PolicyJSON: policy,
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (g ChannelGroup) accessPolicyJSON() string {
	count := 0
	if g.ChannelRetryCount != nil && *g.ChannelRetryCount > 0 {
		count = *g.ChannelRetryCount
	}
	raw, errMarshal := json.Marshal(channelGroupPolicyDocument{
		Count:         count,
		StatusCodes:   g.ChannelRetryStatusCodes,
		ErrorContains: g.ChannelRetryErrorContains,
	})
	if errMarshal != nil {
		return `{"count":0}`
	}
	return string(raw)
}

// ChannelRetryLimit returns the number of additional channels a group may try.
// Nil and non-positive values mean no cross-channel switch.
func (g ChannelGroup) ChannelRetryLimit() int {
	if g.ChannelRetryCount == nil || *g.ChannelRetryCount <= 0 {
		return 0
	}
	if *g.ChannelRetryCount > MaxChannelRetryCount {
		return MaxChannelRetryCount
	}
	return *g.ChannelRetryCount
}
