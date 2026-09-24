package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

const (
	streamTimeoutHandshake = `data: {"choices":[{"delta":{"role":"assistant"}}]}`
	streamTimeoutToken     = `data: {"choices":[{"delta":{"content":"hi"}}]}`
)

func TestReadUntilFirstStreamTokenIgnoresHandshake(t *testing.T) {
	chunks := make(chan cliproxyexecutor.StreamChunk, 2)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutToken)}
	close(chunks)

	buffered, err := readUntilFirstStreamToken(context.Background(), chunks, time.Now().Add(time.Second), sdktranslator.FormatOpenAI)
	if err != nil {
		t.Fatalf("readUntilFirstStreamToken() error = %v", err)
	}
	if len(buffered) != 2 {
		t.Fatalf("buffered = %d, want handshake and token", len(buffered))
	}
}

func TestReadUntilFirstStreamTokenTimesOut(t *testing.T) {
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}

	_, err := readUntilFirstStreamToken(context.Background(), chunks, time.Now().Add(150*time.Millisecond), sdktranslator.FormatOpenAI)
	if !isStreamFirstTokenTimeout(err) {
		t.Fatalf("error = %v, want first-token timeout", err)
	}
	if statusCodeFromError(err) != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", statusCodeFromError(err))
	}
}

func TestReadUntilFirstStreamTokenCancelsAtBufferLimit(t *testing.T) {
	chunks := make(chan cliproxyexecutor.StreamChunk, streamFirstTokenBufferLimit+2)
	for i := 0; i < streamFirstTokenBufferLimit+2; i++ {
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}
	}
	close(chunks)

	buffered, err := readUntilFirstStreamToken(context.Background(), chunks, time.Now().Add(time.Second), sdktranslator.FormatOpenAI)
	if !isStreamFirstTokenTimeout(err) {
		t.Fatalf("error = %v, want first-token timeout", err)
	}
	if len(buffered) != 0 {
		t.Fatalf("buffered = %d, want the preamble discarded", len(buffered))
	}
}

func TestExecuteStreamUntilFirstTokenCancelsUpstream(t *testing.T) {
	canceled := make(chan bool, 1)
	_, err := executeStreamUntilFirstToken(context.Background(), 150*time.Millisecond, sdktranslator.FormatOpenAI, func(ctx context.Context) (*cliproxyexecutor.StreamResult, error) {
		chunks := make(chan cliproxyexecutor.StreamChunk)
		go func() {
			defer close(chunks)
			select {
			case <-ctx.Done():
				canceled <- cliproxyexecutor.StreamFirstTokenTimeoutCanceled(ctx)
				return
			case chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}:
			}
			<-ctx.Done()
			canceled <- cliproxyexecutor.StreamFirstTokenTimeoutCanceled(ctx)
		}()
		return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
	})
	if !isStreamFirstTokenTimeout(err) {
		t.Fatalf("error = %v, want first-token timeout", err)
	}
	select {
	case got := <-canceled:
		if !got {
			t.Fatal("upstream context was not canceled for first-token timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream cancel")
	}
}

func TestExecuteStreamUntilFirstTokenKeepsParentCancel(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	canceled := make(chan bool, 1)
	done := make(chan error, 1)
	go func() {
		_, err := executeStreamUntilFirstToken(parent, time.Second, sdktranslator.FormatOpenAI, func(ctx context.Context) (*cliproxyexecutor.StreamResult, error) {
			chunks := make(chan cliproxyexecutor.StreamChunk)
			go func() {
				defer close(chunks)
				<-ctx.Done()
				canceled <- cliproxyexecutor.StreamFirstTokenTimeoutCanceled(ctx)
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		})
		done <- err
	}()
	cancelParent()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || isStreamFirstTokenTimeout(err) {
			t.Fatalf("error = %v, want parent cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for parent cancellation")
	}
	select {
	case got := <-canceled:
		if got {
			t.Fatal("parent cancellation was reported as a first-token timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream cancel")
	}
}

type streamTimeoutExecutor struct {
	mu       sync.Mutex
	ids      []string
	canceled chan bool
}

func (e *streamTimeoutExecutor) Identifier() string { return "codex" }
func (e *streamTimeoutExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e *streamTimeoutExecutor) ExecuteStream(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.mu.Unlock()
	if id == "a-slow" && (auth == nil || auth.StreamFirstTokenTimeout() <= 0) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if id == "a-slow" {
		chunks := make(chan cliproxyexecutor.StreamChunk)
		go func() {
			defer close(chunks)
			select {
			case <-ctx.Done():
				if e.canceled != nil {
					e.canceled <- cliproxyexecutor.StreamFirstTokenTimeoutCanceled(ctx)
				}
				return
			case chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}:
			}
			<-ctx.Done()
			if e.canceled != nil {
				e.canceled <- cliproxyexecutor.StreamFirstTokenTimeoutCanceled(ctx)
			}
		}()
		return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutToken)}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (e *streamTimeoutExecutor) Refresh(context.Context, *Auth) (*Auth, error) { return nil, nil }
func (e *streamTimeoutExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e *streamTimeoutExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}
func (e *streamTimeoutExecutor) calledIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.ids))
	copy(out, e.ids)
	return out
}

func registerStreamTimeoutAuth(t *testing.T, manager *Manager, id string, metadata map[string]any, model string) {
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

func TestExecuteStreamSwitchesChannelAfterFirstTokenTimeout(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &streamTimeoutExecutor{canceled: make(chan bool, 1)}
	manager.RegisterExecutor(executor)
	model := "gpt-first-token-timeout"
	registerStreamTimeoutAuth(t, manager, "a-slow", map[string]any{
		"stream_first_token_timeout_seconds": 1,
		"provider_retry_count":               3,
		"provider_retry_status_codes":        []int{http.StatusGatewayTimeout},
		"disable_cooling":                    true,
	}, model)
	registerStreamTimeoutAuth(t, manager, "b-fast", map[string]any{"disable_cooling": true}, model)

	result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
		Stream:       true,
		SourceFormat: sdktranslator.FormatOpenAI,
	})
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "a-slow" || got[1] != "b-fast" {
		t.Fatalf("calls = %#v, want a-slow then b-fast", got)
	}
	select {
	case canceled := <-executor.canceled:
		if !canceled {
			t.Fatal("slow channel was not canceled for first-token timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for slow channel cancel")
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), "hi") {
			t.Fatalf("chunk = %#v, want token hi", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failover token")
	}
}

func TestExecuteStreamFirstTokenTimeoutRespectsChannelGroupBudget(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &streamTimeoutExecutor{}
	manager.RegisterExecutor(executor)
	model := "gpt-first-token-group"
	registerChannelGroupAuth(t, manager, "a-slow", "team", "2", map[string]any{
		"stream_first_token_timeout_seconds": 1,
		"disable_cooling":                    true,
	}, model)
	registerChannelGroupAuth(t, manager, "b-fast", "team", "1", map[string]any{"disable_cooling": true}, model)

	opts := channelGroupOptions(1, []int{http.StatusBadRequest}, nil)
	opts.Stream = true
	opts.SourceFormat = sdktranslator.FormatOpenAI
	result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, opts)
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "a-slow" || got[1] != "b-fast" {
		t.Fatalf("calls = %#v, want group failover despite unmatched status list", got)
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), "hi") {
			t.Fatalf("chunk = %#v, want token hi", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failover token")
	}
}

type passthroughHandshakeExecutor struct {
	release <-chan struct{}
}

func (e *passthroughHandshakeExecutor) Identifier() string { return "codex" }
func (e *passthroughHandshakeExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e *passthroughHandshakeExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	chunks := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(chunks)
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutHandshake)}
		<-e.release
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(streamTimeoutToken)}
	}()
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (e *passthroughHandshakeExecutor) Refresh(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}
func (e *passthroughHandshakeExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e *passthroughHandshakeExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestExecuteStreamWithoutFirstTokenTimeoutForwardsHandshakeImmediately(t *testing.T) {
	release := make(chan struct{})
	manager := NewManager(nil, providerRetrySelector{}, nil)
	manager.RegisterExecutor(&passthroughHandshakeExecutor{release: release})
	model := "gpt-first-token-passthrough"
	registerStreamTimeoutAuth(t, manager, "plain", map[string]any{"disable_cooling": true}, model)

	done := make(chan *cliproxyexecutor.StreamResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
			Stream:       true,
			SourceFormat: sdktranslator.FormatOpenAI,
		})
		if errExecute != nil {
			errCh <- errExecute
			return
		}
		done <- result
	}()

	var result *cliproxyexecutor.StreamResult
	select {
	case errExecute := <-errCh:
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("ExecuteStream blocked on a non-token chunk while first-token timeout is disabled")
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), `"role":"assistant"`) {
			t.Fatalf("chunk = %#v, want the handshake", chunk)
		}
		if strings.Contains(string(chunk.Payload), "hi") {
			t.Fatal("substantive token was required before the handshake could be forwarded")
		}
	case <-time.After(time.Second):
		t.Fatal("handshake was not forwarded")
	}
	close(release)
}

func TestExecuteStreamDisabledFirstTokenTimeoutWaits(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &streamTimeoutExecutor{}
	manager.RegisterExecutor(executor)
	model := "gpt-first-token-disabled"
	registerStreamTimeoutAuth(t, manager, "a-slow", map[string]any{"disable_cooling": true}, model)
	registerStreamTimeoutAuth(t, manager, "b-fast", map[string]any{"disable_cooling": true}, model)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, errExecute := manager.ExecuteStream(ctx, []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
		Stream:       true,
		SourceFormat: sdktranslator.FormatOpenAI,
	})
	if !errors.Is(errExecute, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the disabled wait to follow the parent deadline", errExecute)
	}
	if got := executor.calledIDs(); len(got) != 1 || got[0] != "a-slow" {
		t.Fatalf("calls = %#v, want only a-slow", got)
	}
}

func TestExecuteStreamParentCancelDoesNotSwitchChannel(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &streamTimeoutExecutor{canceled: make(chan bool, 1)}
	manager.RegisterExecutor(executor)
	model := "gpt-first-token-parent-cancel"
	registerStreamTimeoutAuth(t, manager, "a-slow", map[string]any{
		"stream_first_token_timeout_seconds": 30,
		"disable_cooling":                    true,
	}, model)
	registerStreamTimeoutAuth(t, manager, "b-fast", map[string]any{"disable_cooling": true}, model)

	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	done := make(chan error, 1)
	go func() {
		_, errExecute := manager.ExecuteStream(parent, []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{
			Stream:       true,
			SourceFormat: sdktranslator.FormatOpenAI,
		})
		done <- errExecute
	}()
	select {
	case <-executor.canceled:
		t.Fatal("parent cancel was reported before the request was canceled")
	case <-time.After(50 * time.Millisecond):
	}
	cancelParent()
	select {
	case errExecute := <-done:
		if !errors.Is(errExecute, context.Canceled) || isStreamFirstTokenTimeout(errExecute) {
			t.Fatalf("error = %v, want parent cancellation", errExecute)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for parent cancellation")
	}
	if got := executor.calledIDs(); len(got) != 1 || got[0] != "a-slow" {
		t.Fatalf("calls = %#v, want only a-slow", got)
	}
}

func TestExecuteStreamFirstTokenTimeoutDoesNotCoolChannel(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &streamTimeoutExecutor{}
	manager.RegisterExecutor(executor)
	model := "gpt-first-token-cooldown"
	registerStreamTimeoutAuth(t, manager, "a-slow", map[string]any{
		"stream_first_token_timeout_seconds": 1,
	}, model)
	registerStreamTimeoutAuth(t, manager, "b-fast", map[string]any{}, model)

	opts := cliproxyexecutor.Options{Stream: true, SourceFormat: sdktranslator.FormatOpenAI}
	result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, opts)
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), "hi") {
			t.Fatalf("chunk = %#v, want token hi", chunk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for failover token")
	}
	slow, ok := manager.GetByID("a-slow")
	if !ok {
		t.Fatal("slow channel missing after timeout")
	}
	if slow.Unavailable || !slow.NextRetryAfter.IsZero() {
		t.Fatalf("slow channel cooled: unavailable=%v next=%v", slow.Unavailable, slow.NextRetryAfter)
	}
	for key, state := range slow.ModelStates {
		if state != nil && (state.Unavailable || !state.NextRetryAfter.IsZero()) {
			t.Fatalf("model %s cooled: unavailable=%v next=%v", key, state.Unavailable, state.NextRetryAfter)
		}
	}

	before := len(executor.calledIDs())
	result, errExecute = manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, opts)
	if errExecute != nil {
		t.Fatalf("second ExecuteStream() error = %v", errExecute)
	}
	if got := executor.calledIDs(); len(got) <= before || got[before] != "a-slow" {
		t.Fatalf("calls = %#v, want the slow channel selected again at %d", got, before)
	}
	select {
	case chunk := <-result.Chunks:
		if chunk.Err != nil || !strings.Contains(string(chunk.Payload), "hi") {
			t.Fatalf("second chunk = %#v, want token hi", chunk)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the second failover token")
	}
}
