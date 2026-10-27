package auth

import (
	"net/http"
	"strings"
)

func errOAuthCredentialsUnsupported() error {
	return &Error{Code: "oauth_unsupported", Message: "OAuth account credentials are no longer supported", HTTPStatus: http.StatusBadRequest}
}

// IsUnsupportedOAuthAuth reports leftover account-OAuth records that must not
// enter the scheduler, even if a remote store still lists them.
func IsUnsupportedOAuthAuth(auth *Auth) bool {
	return auth != nil && auth.AuthKind() == AuthKindOAuth
}

const (
	AuthKindAPIKey = "apikey"
	AuthKindOAuth  = "oauth"

	AuthSourceConfig      = "config"
	AuthSourceFile        = "file"
	AuthSourceGit         = "git"
	AuthSourceMemory      = "memory"
	AuthSourceObjectStore = "objectstore"
	AuthSourcePostgres    = "postgres"

	AttributeAPIKey           = "api_key"
	AttributeAuthKind         = "auth_kind"
	AttributeCodexAlphaSearch = "codex_alpha_search"
	AttributeConfigIndex      = "config_index"
	AttributePath             = "path"
	AttributeRuntimeOnly      = "runtime_only"
	AttributeSource           = "source"
	AttributeSourceBackend    = "source_backend"
	AttributeWeight           = "weight"
)

// AuthKind returns the credential kind using explicit metadata first and legacy
// field-shape fallbacks second.
func (a *Auth) AuthKind() string {
	if a == nil {
		return ""
	}
	if kind := normalizeAuthKind(authAttribute(a, AttributeAuthKind)); kind != "" {
		return kind
	}
	if kind := normalizeAuthKind(authMetadataString(a, AttributeAuthKind)); kind != "" {
		return kind
	}
	if authAttribute(a, AttributeAPIKey) != "" {
		return AuthKindAPIKey
	}
	if authHasServiceAccountMetadata(a) {
		return AuthKindAPIKey
	}
	if authHasOAuthMetadata(a) {
		return AuthKindOAuth
	}
	return ""
}

// AuthSourceKind returns where the Auth entry came from at runtime.
func (a *Auth) AuthSourceKind() string {
	if a == nil {
		return ""
	}
	if strings.EqualFold(authAttribute(a, AttributeRuntimeOnly), "true") {
		return AuthSourceMemory
	}
	if source := normalizeAuthSourceKind(authAttribute(a, AttributeSourceBackend)); source != "" {
		return source
	}
	source := authAttribute(a, AttributeSource)
	if source != "" {
		sourceLower := strings.ToLower(source)
		if strings.HasPrefix(sourceLower, AuthSourceConfig+":") {
			return AuthSourceConfig
		}
		if normalized := normalizeAuthSourceKind(source); normalized != "" {
			return normalized
		}
		return AuthSourceFile
	}
	if authAttribute(a, AttributePath) != "" {
		return AuthSourceFile
	}
	if strings.TrimSpace(a.FileName) != "" {
		return AuthSourceFile
	}
	return ""
}

func normalizeAuthKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case AuthKindAPIKey, "api_key", "api-key":
		return AuthKindAPIKey
	case AuthKindOAuth, "oauth2":
		return AuthKindOAuth
	default:
		return ""
	}
}

func normalizeAuthSourceKind(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case AuthSourceConfig:
		return AuthSourceConfig
	case AuthSourceFile, "filesystem":
		return AuthSourceFile
	case AuthSourceGit:
		return AuthSourceGit
	case AuthSourceMemory, "runtime", "runtime_only":
		return AuthSourceMemory
	case AuthSourceObjectStore, "object-store":
		return AuthSourceObjectStore
	case AuthSourcePostgres, "postgresql", "database", "db":
		return AuthSourcePostgres
	default:
		return ""
	}
}

func authHasServiceAccountMetadata(auth *Auth) bool {
	if auth == nil || len(auth.Metadata) == 0 {
		return false
	}
	raw, ok := auth.Metadata["service_account"]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case map[string]any:
		return len(value) > 0
	case string:
		return strings.TrimSpace(value) != ""
	default:
		return false
	}
}

func authHasOAuthMetadata(auth *Auth) bool {
	if auth == nil || len(auth.Metadata) == 0 {
		return false
	}
	if authHasServiceAccountMetadata(auth) {
		return false
	}
	for _, key := range []string{"access_token", "refresh_token", "id_token"} {
		if authMetadataString(auth, key) != "" {
			return true
		}
	}
	if token, ok := auth.Metadata["token"].(map[string]any); ok && len(token) > 0 {
		if authMetadataStringFromMap(token, "access_token") != "" || authMetadataStringFromMap(token, "refresh_token") != "" || authMetadataStringFromMap(token, "id_token") != "" {
			return true
		}
	}
	return false
}

func authMetadataStringFromMap(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func authAttribute(auth *Auth, key string) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes[key])
}

func authMetadataString(auth *Auth, key string) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	switch value := auth.Metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}
