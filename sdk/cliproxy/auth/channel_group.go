package auth

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const (
	// AttributeChannelGroup is the channel's group name. Empty means ungrouped.
	AttributeChannelGroup = "group"
	// AttributeChannelPanel is the management panel that owns the channel group.
	AttributeChannelPanel = "channel_panel"

	// ChannelGroupProviderMetadataKey selects the provider panel for a group API key.
	ChannelGroupProviderMetadataKey = "channel-group-provider"
	// ChannelGroupMetadataKey selects the group name for a group API key.
	ChannelGroupMetadataKey = "channel-group"
	// ChannelGroupPolicyMetadataKey carries the group's cross-channel retry policy JSON.
	ChannelGroupPolicyMetadataKey = "channel-group-policy"
)

type channelGroupPolicyDocument struct {
	Count         int      `json:"count"`
	StatusCodes   []int    `json:"status-codes"`
	ErrorContains []string `json:"error-contains"`
}

type channelGroupPolicy struct {
	scoped        bool
	panel         string
	group         string
	count         int
	statusCodes   map[int]struct{}
	errorContains []string
}

func channelGroupPolicyFromOptions(opts cliproxyexecutor.Options) channelGroupPolicy {
	return channelGroupPolicyFromMetadata(opts.Metadata)
}

func channelGroupPolicyFromMetadata(metadata map[string]any) channelGroupPolicy {
	panel := metadataString(metadata, ChannelGroupProviderMetadataKey)
	group := metadataString(metadata, ChannelGroupMetadataKey)
	if panel == "" || group == "" {
		return channelGroupPolicy{}
	}
	policy := channelGroupPolicy{scoped: true, panel: panel, group: group}
	raw := metadataString(metadata, ChannelGroupPolicyMetadataKey)
	if raw == "" {
		return policy
	}
	var document channelGroupPolicyDocument
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return policy
	}
	if document.Count > 0 {
		policy.count = document.Count
	}
	if len(document.StatusCodes) > 0 {
		policy.statusCodes = make(map[int]struct{}, len(document.StatusCodes))
		for _, code := range document.StatusCodes {
			if code >= 100 && code <= 599 {
				policy.statusCodes[code] = struct{}{}
			}
		}
	}
	for _, phrase := range document.ErrorContains {
		trimmed := strings.TrimSpace(phrase)
		if trimmed == "" {
			continue
		}
		policy.errorContains = append(policy.errorContains, trimmed)
	}
	return policy
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (p channelGroupPolicy) matches(err error) bool {
	if !p.scoped || err == nil {
		return false
	}
	if len(p.statusCodes) == 0 && len(p.errorContains) == 0 {
		return false
	}
	status := statusCodeFromError(err)
	if status != 0 {
		if _, ok := p.statusCodes[status]; ok {
			return true
		}
	}
	if len(p.errorContains) == 0 {
		return false
	}
	text := channelGroupErrorText(err)
	for _, phrase := range p.errorContains {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func channelGroupErrorText(err error) string {
	if err == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(err.Error())
	type bodyProvider interface{ ResponseBody() []byte }
	var body bodyProvider
	if errors.As(err, &body) && body != nil {
		if payload := body.ResponseBody(); len(payload) > 0 {
			builder.WriteByte('\n')
			builder.Write(payload)
		}
	}
	return builder.String()
}

func applyChannelGroupRetryBudget(opts cliproxyexecutor.Options, requestRetry, maxCredentials int) (int, int) {
	policy := channelGroupPolicyFromOptions(opts)
	if !policy.scoped {
		return requestRetry, maxCredentials
	}
	count := policy.count
	if count < 0 {
		count = 0
	}
	return 0, 1 + count
}

func stopChannelGroupFailover(opts cliproxyexecutor.Options, err error) bool {
	policy := channelGroupPolicyFromOptions(opts)
	if !policy.scoped || err == nil {
		return false
	}
	return !policy.matches(err)
}

// channelGroupStatusListed reports whether the group's retry status list contains
// this error's HTTP status. A listed status overrides hardcoded stops that would
// otherwise refuse to switch channels. Error-text matches do not.
func channelGroupStatusListed(opts cliproxyexecutor.Options, err error) bool {
	if err == nil {
		return false
	}
	policy := channelGroupPolicyFromOptions(opts)
	if !policy.scoped || len(policy.statusCodes) == 0 {
		return false
	}
	status := statusCodeFromError(err)
	if status == 0 {
		return false
	}
	_, ok := policy.statusCodes[status]
	return ok
}

// AuthIDsForChannelGroup returns enabled auth IDs that belong to one panel group.
func (m *Manager) AuthIDsForChannelGroup(panel, group string) []string {
	panel = strings.TrimSpace(panel)
	group = strings.TrimSpace(group)
	if m == nil || panel == "" || group == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0)
	for _, auth := range m.auths {
		if auth == nil || auth.Disabled || auth.Status == StatusDisabled {
			continue
		}
		if auth.Attributes == nil {
			continue
		}
		if auth.Attributes[AttributeChannelGroup] != group || auth.Attributes[AttributeChannelPanel] != panel {
			continue
		}
		if strings.TrimSpace(auth.ID) == "" {
			continue
		}
		ids = append(ids, auth.ID)
	}
	sort.Strings(ids)
	return ids
}

func channelGroupScopeFromMetadata(metadata map[string]any) (string, string) {
	policy := channelGroupPolicyFromMetadata(metadata)
	if !policy.scoped {
		return "", ""
	}
	return policy.panel, policy.group
}
