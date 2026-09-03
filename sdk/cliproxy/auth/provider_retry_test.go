package auth

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestProviderRetryCountAndStatus(t *testing.T) {
	var unset *Auth
	if unset.ProviderRetryCount() != 0 || unset.IsProviderRetryableStatus(http.StatusTooManyRequests) {
		t.Fatal("nil auth should disable same-credential retry")
	}

	auth := &Auth{Metadata: map[string]any{"provider_retry_count": 3}}
	if auth.ProviderRetryCount() != 3 {
		t.Fatalf("count = %d, want 3", auth.ProviderRetryCount())
	}
	if !auth.IsProviderRetryableStatus(http.StatusUnauthorized) || !auth.IsProviderRetryableStatus(http.StatusForbidden) || !auth.IsProviderRetryableStatus(http.StatusTooManyRequests) {
		t.Fatal("omitted codes should default to 401/403/429")
	}
	if auth.IsProviderRetryableStatus(http.StatusBadGateway) {
		t.Fatal("502 should not match default codes")
	}

	auth = &Auth{Metadata: map[string]any{
		"provider_retry_count":        2,
		"provider_retry_status_codes": []int{},
	}}
	if auth.IsProviderRetryableStatus(http.StatusTooManyRequests) {
		t.Fatal("explicit empty codes should disable status-code retry")
	}

	auth = &Auth{Metadata: map[string]any{
		"provider-retry-count":        12,
		"provider-retry-status-codes": []any{500, 502},
	}}
	if auth.ProviderRetryCount() != 10 {
		t.Fatalf("clamped count = %d, want 10", auth.ProviderRetryCount())
	}
	if !auth.IsProviderRetryableStatus(http.StatusBadGateway) {
		t.Fatal("custom 502 should match")
	}
}

type providerRetrySelector struct{}

func (providerRetrySelector) Pick(_ context.Context, _, _ string, _ cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	var selected *Auth
	for _, auth := range auths {
		if auth != nil && (selected == nil || auth.ID < selected.ID) {
			selected = auth
		}
	}
	return selected, nil
}

type providerRetryExecutor struct {
	calls   atomic.Int32
	failFor map[string]int
	status  int
}

func (*providerRetryExecutor) Identifier() string { return "codex" }
func (*providerRetryExecutor) ShouldPrepareRequestAuth(*Auth) bool {
	return false
}
func (*providerRetryExecutor) PrepareRequestAuth(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}
func (e *providerRetryExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.calls.Add(1)
	if auth != nil && e.failFor[auth.ID] > 0 {
		e.failFor[auth.ID]--
		return cliproxyexecutor.Response{}, &Error{HTTPStatus: e.status, Message: "upstream failed"}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}
func (e *providerRetryExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_, err := e.Execute(ctx, auth, req, opts)
	if err != nil {
		return nil, err
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (*providerRetryExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}
func (e *providerRetryExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}
func (*providerRetryExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func TestExecuteRetriesSameCredentialBeforeFailover(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryExecutor{
		failFor: map[string]int{"auth-a": 2},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 2}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q, want ok", resp.Payload)
	}
	if got := executor.calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (2 retries on auth-a then success)", got)
	}
}

func TestExecuteSkipsSameCredentialRetryForUnlistedStatus(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusServiceUnavailable,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-skip"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 3}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if got := executor.calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (immediate failover)", got)
	}
}

func TestExecuteSameCredentialRetryThenNextAuth(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryExecutor{
		failFor: map[string]int{"auth-a": 100},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-next"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 1}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if got := executor.calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (auth-a + 1 retry + auth-b)", got)
	}
}

func TestExecuteRequestScopedStopSkipsSameCredentialRetry(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryExecutor{
		failFor: map[string]int{"auth-a": 100},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-stop"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{
		"provider_retry_count": 3,
		"request_scoped_errors": []any{map[string]any{
			"status": http.StatusTooManyRequests,
			"match":  []string{"upstream failed"},
			"action": "stop",
		}},
	}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute == nil {
		t.Fatal("Execute() error = nil, want request-scoped stop")
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func registerProviderRetryAuth(t *testing.T, manager *Manager, id string, metadata map[string]any, model string) {
	t.Helper()
	if _, errRegister := manager.Register(context.Background(), &Auth{
		ID:       id,
		Provider: "codex",
		Status:   StatusActive,
		Metadata: metadata,
	}); errRegister != nil {
		t.Fatalf("Register(%s) error = %v", id, errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
}
