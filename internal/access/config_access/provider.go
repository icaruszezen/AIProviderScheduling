package configaccess

import (
	"context"
	"net/http"
	"strings"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// GroupCredential binds one client API key to a provider-panel group.
type GroupCredential struct {
	Panel      string
	Group      string
	PolicyJSON string
}

// Register ensures the config-access provider is available to the access manager.
// Global api-keys keep the existing result. Group keys add panel and retry metadata.
func Register(apiKeys []string, groups map[string]GroupCredential) {
	keys := normalizeKeys(apiKeys)
	groupKeys := normalizeGroupCredentials(groups)
	if len(keys) == 0 && len(groupKeys) == 0 {
		sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
		return
	}

	sdkaccess.RegisterProvider(
		sdkaccess.AccessProviderTypeConfigAPIKey,
		newProvider(sdkaccess.DefaultAccessProviderName, keys, groupKeys),
	)
}

type provider struct {
	name   string
	keys   map[string]struct{}
	groups map[string]GroupCredential
}

func newProvider(name string, keys []string, groups map[string]GroupCredential) *provider {
	providerName := strings.TrimSpace(name)
	if providerName == "" {
		providerName = sdkaccess.DefaultAccessProviderName
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	return &provider{name: providerName, keys: keySet, groups: groups}
}

func (p *provider) Identifier() string {
	if p == nil || p.name == "" {
		return sdkaccess.DefaultAccessProviderName
	}
	return p.name
}

func (p *provider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	if len(p.keys) == 0 && len(p.groups) == 0 {
		return nil, sdkaccess.NewNotHandledError()
	}
	authHeader := r.Header.Get("Authorization")
	authHeaderGoogle := r.Header.Get("X-Goog-Api-Key")
	authHeaderAnthropic := r.Header.Get("X-Api-Key")
	queryKey := ""
	queryAuthToken := ""
	if r.URL != nil {
		queryKey = r.URL.Query().Get("key")
		queryAuthToken = r.URL.Query().Get("auth_token")
	}
	if authHeader == "" && authHeaderGoogle == "" && authHeaderAnthropic == "" && queryKey == "" && queryAuthToken == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}

	apiKey := extractBearerToken(authHeader)

	candidates := []struct {
		value  string
		source string
	}{
		{apiKey, "authorization"},
		{authHeaderGoogle, "x-goog-api-key"},
		{authHeaderAnthropic, "x-api-key"},
		{queryKey, "query-key"},
		{queryAuthToken, "query-auth-token"}}

	for _, candidate := range candidates {
		if candidate.value == "" {
			continue
		}
		if _, ok := p.keys[candidate.value]; ok {
			return &sdkaccess.Result{
				Provider:  p.Identifier(),
				Principal: candidate.value,
				Metadata: map[string]string{
					"source": candidate.source}}, nil
		}
		if group, ok := p.groups[candidate.value]; ok {
			return &sdkaccess.Result{
				Provider:  p.Identifier(),
				Principal: candidate.value,
				Metadata: map[string]string{
					"source":                                 candidate.source,
					coreauth.ChannelGroupProviderMetadataKey: group.Panel,
					coreauth.ChannelGroupMetadataKey:         group.Group,
					coreauth.ChannelGroupPolicyMetadataKey:   group.PolicyJSON}}, nil
		}
	}

	return nil, sdkaccess.NewInvalidCredentialError()
}

func extractBearerToken(header string) string {
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return header
	}
	if strings.ToLower(parts[0]) != "bearer" {
		return header
	}
	return strings.TrimSpace(parts[1])
}

func normalizeGroupCredentials(groups map[string]GroupCredential) map[string]GroupCredential {
	if len(groups) == 0 {
		return nil
	}
	out := make(map[string]GroupCredential, len(groups))
	for key, group := range groups {
		trimmed := strings.TrimSpace(key)
		panel := strings.TrimSpace(group.Panel)
		name := strings.TrimSpace(group.Group)
		if trimmed == "" || panel == "" || name == "" {
			continue
		}
		out[trimmed] = GroupCredential{
			Panel:      panel,
			Group:      name,
			PolicyJSON: strings.TrimSpace(group.PolicyJSON),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if _, exists := seen[trimmedKey]; exists {
			continue
		}
		seen[trimmedKey] = struct{}{}
		normalized = append(normalized, trimmedKey)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
