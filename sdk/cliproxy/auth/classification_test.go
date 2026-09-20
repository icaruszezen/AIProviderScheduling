package auth

import (
	"context"
	"errors"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestAuthKind(t *testing.T) {
	tests := []struct {
		name string
		auth *Auth
		want string
	}{
		{
			name: "explicit api key attribute",
			auth: &Auth{Attributes: map[string]string{AttributeAuthKind: "api_key"}},
			want: AuthKindAPIKey},
		{
			name: "explicit oauth attribute wins over api key fallback",
			auth: &Auth{Attributes: map[string]string{AttributeAuthKind: "oauth", AttributeAPIKey: "k"}},
			want: AuthKindOAuth},
		{
			name: "explicit oauth metadata",
			auth: &Auth{Metadata: map[string]any{AttributeAuthKind: "oauth"}},
			want: AuthKindOAuth},
		{
			name: "legacy api key attribute",
			auth: &Auth{Attributes: map[string]string{AttributeAPIKey: "k"}},
			want: AuthKindAPIKey},
		{
			name: "legacy oauth metadata",
			auth: &Auth{Metadata: map[string]any{"access_token": "token"}},
			want: AuthKindOAuth},
		{
			name: "email alone is not oauth",
			auth: &Auth{Metadata: map[string]any{"email": "aistudio-channel"}},
			want: ""},
		{
			name: "service account with email is api key",
			auth: &Auth{Metadata: map[string]any{
				"email":           "sa@project.iam.gserviceaccount.com",
				"service_account": map[string]any{"private_key": "k", "project_id": "p"}}},
			want: AuthKindAPIKey},
		{
			name: "unknown metadata shape",
			auth: &Auth{Metadata: map[string]any{"type": "test"}},
			want: ""}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.auth.AuthKind(); got != tt.want {
				t.Fatalf("AuthKind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthSourceKind(t *testing.T) {
	tests := []struct {
		name string
		auth *Auth
		want string
	}{
		{
			name: "runtime only memory",
			auth: &Auth{Attributes: map[string]string{AttributeRuntimeOnly: "true", AttributeSourceBackend: AuthSourcePostgres}},
			want: AuthSourceMemory},
		{
			name: "backend postgres",
			auth: &Auth{Attributes: map[string]string{AttributeSourceBackend: "postgresql", AttributePath: "/tmp/auth.json"}},
			want: AuthSourcePostgres},
		{
			name: "backend object store",
			auth: &Auth{Attributes: map[string]string{AttributeSourceBackend: "object-store", AttributePath: "/tmp/auth.json"}},
			want: AuthSourceObjectStore},
		{
			name: "config source",
			auth: &Auth{Attributes: map[string]string{AttributeSource: "config:codex[abc]"}},
			want: AuthSourceConfig},
		{
			name: "path source",
			auth: &Auth{Attributes: map[string]string{AttributeSource: "/tmp/auth.json"}},
			want: AuthSourceFile},
		{
			name: "path attribute",
			auth: &Auth{Attributes: map[string]string{AttributePath: "/tmp/auth.json"}},
			want: AuthSourceFile},
		{
			name: "filename fallback",
			auth: &Auth{FileName: "codex.json"},
			want: AuthSourceFile}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.auth.AuthSourceKind(); got != tt.want {
				t.Fatalf("AuthSourceKind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAccountInfoUsesAuthKind(t *testing.T) {
	apiKeyAuth := &Auth{Attributes: map[string]string{AttributeAuthKind: "api-key", AttributeAPIKey: "k"}}
	kind, value := apiKeyAuth.AccountInfo()
	if kind != "api_key" || value != "k" {
		t.Fatalf("api key AccountInfo() = %q, %q", kind, value)
	}

	oauthAuth := &Auth{
		Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth, AttributeAPIKey: "k"},
		Metadata:   map[string]any{"email": "user@example.com"}}
	kind, value = oauthAuth.AccountInfo()
	if kind != "oauth" || value != "user@example.com" {
		t.Fatalf("oauth AccountInfo() = %q, %q", kind, value)
	}

	oauthWithoutEmail := &Auth{Metadata: map[string]any{"access_token": "token"}}
	kind, value = oauthWithoutEmail.AccountInfo()
	if kind != "oauth" || value != "" {
		t.Fatalf("oauth without email AccountInfo() = %q, %q", kind, value)
	}
}

func TestRegisterAndSelectFailClosedForOAuth(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.executors["codex"] = schedulerTestExecutor{}

	registered, errRegister := manager.Register(context.Background(), &Auth{
		ID:       "oauth-cred",
		Provider: "codex",
		Metadata: map[string]any{"access_token": "token"}})
	if registered != nil {
		t.Fatalf("Register() auth = %#v, want nil", registered)
	}
	var authErr *Error
	if !errors.As(errRegister, &authErr) || authErr.Code != "oauth_unsupported" {
		t.Fatalf("Register() error = %#v, want oauth_unsupported", errRegister)
	}

	explicit := &Auth{
		ID:         "oauth-explicit",
		Provider:   "codex",
		Attributes: map[string]string{AttributeAuthKind: AuthKindOAuth, AttributeAPIKey: "k"}}
	if _, errExplicit := manager.Register(context.Background(), explicit); !errors.As(errExplicit, &authErr) || authErr.Code != "oauth_unsupported" {
		t.Fatalf("Register(explicit oauth) error = %#v, want oauth_unsupported", errExplicit)
	}

	selected, errSelect := manager.SelectAuthByKind(context.Background(), "codex", "", AuthKindOAuth, cliproxyexecutor.Options{})
	if selected != nil {
		t.Fatalf("SelectAuthByKind() auth = %#v, want nil", selected)
	}
	if !errors.As(errSelect, &authErr) || authErr.Code != "oauth_unsupported" {
		t.Fatalf("SelectAuthByKind() error = %#v, want oauth_unsupported", errSelect)
	}

	apiKeyAuth := &Auth{ID: "apikey-cred", Provider: "codex", Attributes: map[string]string{AttributeAPIKey: "k"}}
	if _, errRegister := manager.Register(context.Background(), apiKeyAuth); errRegister != nil {
		t.Fatalf("Register(api key) error = %v", errRegister)
	}
	oauthUpdate := &Auth{ID: "apikey-cred", Provider: "codex", Metadata: map[string]any{"access_token": "token"}}
	if _, errUpdate := manager.Update(context.Background(), oauthUpdate); !errors.As(errUpdate, &authErr) || authErr.Code != "oauth_unsupported" {
		t.Fatalf("Update(oauth) error = %#v, want oauth_unsupported", errUpdate)
	}
}

func TestManagerLoadSkipsOAuthRecords(t *testing.T) {
	store := NewMemoryStore()
	if _, errSave := store.Save(context.Background(), &Auth{
		ID:       "oauth-leftover",
		Provider: "codex",
		Metadata: map[string]any{"access_token": "token"},
	}); errSave != nil {
		t.Fatalf("Save(oauth) error = %v", errSave)
	}
	if _, errSave := store.Save(context.Background(), &Auth{
		ID:         "apikey-keep",
		Provider:   "codex",
		Attributes: map[string]string{AttributeAPIKey: "k"},
	}); errSave != nil {
		t.Fatalf("Save(api key) error = %v", errSave)
	}

	manager := NewManager(store, &RoundRobinSelector{}, nil)
	if errLoad := manager.Load(context.Background()); errLoad != nil {
		t.Fatalf("Load() error = %v", errLoad)
	}

	loaded := manager.List()
	if len(loaded) != 1 || loaded[0].ID != "apikey-keep" {
		t.Fatalf("Load() auths = %#v, want only apikey-keep", loaded)
	}
}
