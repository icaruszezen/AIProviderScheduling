package auth

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// providerRetryStreamExecutor fails a configured number of times per credential.
// failAtBootstrap decides whether the failure surfaces from ExecuteStream itself
// or from the first chunk, which are two distinct retry sites in the streaming
// conductor.
type providerRetryStreamExecutor struct {
	calls           atomic.Int32
	failFor         map[string]int
	status          int
	failAtBootstrap bool
}

func (*providerRetryStreamExecutor) Identifier() string { return "codex" }

func (*providerRetryStreamExecutor) ShouldPrepareRequestAuth(*Auth) bool { return false }

func (*providerRetryStreamExecutor) PrepareRequestAuth(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}

func (e *providerRetryStreamExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.calls.Add(1)
	if auth != nil && e.failFor[auth.ID] > 0 {
		e.failFor[auth.ID]--
		return cliproxyexecutor.Response{}, &Error{HTTPStatus: e.status, Message: "upstream failed"}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *providerRetryStreamExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	e.calls.Add(1)
	failing := auth != nil && e.failFor[auth.ID] > 0
	if failing {
		e.failFor[auth.ID]--
	}
	err := &Error{HTTPStatus: e.status, Message: "upstream failed"}
	if failing && !e.failAtBootstrap {
		return nil, err
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	if failing {
		chunks <- cliproxyexecutor.StreamChunk{Err: err}
	} else {
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}

func (*providerRetryStreamExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *providerRetryStreamExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (*providerRetryStreamExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func drainStream(t *testing.T, result *cliproxyexecutor.StreamResult) error {
	t.Helper()
	if result == nil {
		t.Fatal("stream result = nil, want a stream")
	}
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			return chunk.Err
		}
	}
	return nil
}

func TestExecuteStreamRetriesSameCredentialBeforeFailover(t *testing.T) {
	for _, failAtBootstrap := range []bool{false, true} {
		name := "connect failure"
		if failAtBootstrap {
			name = "first chunk failure"
		}
		t.Run(name, func(t *testing.T) {
			manager := NewManager(nil, providerRetrySelector{}, nil)
			executor := &providerRetryStreamExecutor{
				failFor:         map[string]int{"auth-a": 2},
				status:          http.StatusTooManyRequests,
				failAtBootstrap: failAtBootstrap,
			}
			manager.RegisterExecutor(executor)
			model := "gpt-provider-retry-stream-" + name
			registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 2}, model)
			registerProviderRetryAuth(t, manager, "auth-b", nil, model)

			chunks, errStream := manager.ExecuteStream(
				context.Background(),
				[]string{"codex"},
				cliproxyexecutor.Request{Model: model},
				cliproxyexecutor.Options{Stream: true},
			)
			if errStream != nil {
				t.Fatalf("ExecuteStream() error = %v", errStream)
			}
			if errDrain := drainStream(t, chunks); errDrain != nil {
				t.Fatalf("stream error = %v, want a clean stream after the retries", errDrain)
			}
			if got := executor.calls.Load(); got != 3 {
				t.Fatalf("upstream calls = %d, want 3 (2 retries on auth-a then success)", got)
			}
		})
	}
}

func TestExecuteStreamSkipsSameCredentialRetryForUnlistedStatus(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryStreamExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusServiceUnavailable,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-stream-skip"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 3}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	chunks, errStream := manager.ExecuteStream(
		context.Background(),
		[]string{"codex"},
		cliproxyexecutor.Request{Model: model},
		cliproxyexecutor.Options{Stream: true},
	)
	if errStream != nil {
		t.Fatalf("ExecuteStream() error = %v", errStream)
	}
	if errDrain := drainStream(t, chunks); errDrain != nil {
		t.Fatalf("stream error = %v, want failover to auth-b", errDrain)
	}
	if got := executor.calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (immediate failover)", got)
	}
}

func TestExecuteStreamSameCredentialRetryThenNextAuth(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryStreamExecutor{
		failFor: map[string]int{"auth-a": 100},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-stream-next"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{"provider_retry_count": 1}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	chunks, errStream := manager.ExecuteStream(
		context.Background(),
		[]string{"codex"},
		cliproxyexecutor.Request{Model: model},
		cliproxyexecutor.Options{Stream: true},
	)
	if errStream != nil {
		t.Fatalf("ExecuteStream() error = %v", errStream)
	}
	if errDrain := drainStream(t, chunks); errDrain != nil {
		t.Fatalf("stream error = %v, want auth-b to serve the stream", errDrain)
	}
	if got := executor.calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (auth-a + 1 retry + auth-b)", got)
	}
}

func TestExecuteStreamRequestScopedStopSkipsSameCredentialRetry(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &providerRetryStreamExecutor{
		failFor: map[string]int{"auth-a": 100},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-provider-retry-stream-stop"
	registerProviderRetryAuth(t, manager, "auth-a", map[string]any{
		"provider_retry_count": 3,
		"request_scoped_errors": []any{map[string]any{
			"status": http.StatusTooManyRequests,
			"match":  []string{"upstream failed"},
			"action": "stop",
		}},
	}, model)
	registerProviderRetryAuth(t, manager, "auth-b", nil, model)

	if _, errStream := manager.ExecuteStream(
		context.Background(),
		[]string{"codex"},
		cliproxyexecutor.Request{Model: model},
		cliproxyexecutor.Options{Stream: true},
	); errStream == nil {
		t.Fatal("ExecuteStream() error = nil, want request-scoped stop")
	}
	if got := executor.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}
