package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type holdingExecutor struct {
	mu      sync.Mutex
	ids     []string
	entered chan string
	hold    chan struct{}
}

func newHoldingExecutor() *holdingExecutor {
	return &holdingExecutor{
		entered: make(chan string, 4),
		hold:    make(chan struct{}),
	}
}

func (*holdingExecutor) Identifier() string { return "codex" }
func (*holdingExecutor) ShouldPrepareRequestAuth(*Auth) bool {
	return false
}
func (*holdingExecutor) PrepareRequestAuth(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}
func (e *holdingExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.mu.Unlock()
	e.entered <- id
	<-e.hold
	return cliproxyexecutor.Response{Payload: []byte(id)}, nil
}
func (e *holdingExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.mu.Unlock()
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	go func() {
		<-e.hold
		close(chunks)
	}()
	e.entered <- id
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (*holdingExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}
func (e *holdingExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}
func (*holdingExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func executeWithTimeout(t *testing.T, manager *Manager, model string) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		errCh <- errExecute
	}()
	select {
	case errExecute := <-errCh:
		return errExecute
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a full channel to fail over")
	}
	return nil
}

func (e *holdingExecutor) waitEntered(t *testing.T) string {
	t.Helper()
	select {
	case id := <-e.entered:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel execution")
	}
	return ""
}

func registerConnectionAuth(t *testing.T, manager *Manager, id, priority string, limit int, model string) {
	t.Helper()
	metadata := map[string]any{}
	if limit > 0 {
		metadata["max_concurrent_connections"] = limit
	}
	if _, errRegister := manager.Register(context.Background(), &Auth{
		ID:       id,
		Provider: "codex",
		Status:   StatusActive,
		Attributes: map[string]string{
			"priority": priority,
		},
		Metadata: metadata,
	}); errRegister != nil {
		t.Fatalf("Register(%s) error = %v", id, errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
}

func TestChannelConnectionOverflowUsesNextChannel(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := newHoldingExecutor()
	manager.RegisterExecutor(executor)
	model := "gpt-connection-overflow"
	registerConnectionAuth(t, manager, "auth-a", "10", 1, model)
	registerConnectionAuth(t, manager, "auth-b", "1", 1, model)

	errCh := make(chan error, 2)
	go func() {
		_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		errCh <- errExecute
	}()
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("first channel = %q, want auth-a", got)
	}
	if active := manager.ActiveChannelConnections("auth-a"); active != 1 {
		t.Fatalf("auth-a active = %d, want 1", active)
	}

	go func() {
		_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		errCh <- errExecute
	}()
	if got := executor.waitEntered(t); got != "auth-b" {
		t.Fatalf("overflow channel = %q, want auth-b", got)
	}
	close(executor.hold)
	for i := 0; i < 2; i++ {
		if errExecute := <-errCh; errExecute != nil {
			t.Fatalf("Execute() error = %v", errExecute)
		}
	}
	if manager.ActiveChannelConnections("auth-a") != 0 || manager.ActiveChannelConnections("auth-b") != 0 {
		t.Fatalf("active after completion = %d/%d, want 0/0", manager.ActiveChannelConnections("auth-a"), manager.ActiveChannelConnections("auth-b"))
	}
}

func TestChannelConnectionFullDoesNotCoolDown(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := newHoldingExecutor()
	manager.RegisterExecutor(executor)
	model := "gpt-connection-full"
	registerConnectionAuth(t, manager, "auth-a", "10", 1, model)

	done := make(chan error, 1)
	go func() {
		_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		done <- errExecute
	}()
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("first channel = %q, want auth-a", got)
	}

	errExecute := executeWithTimeout(t, manager, model)
	var authErr *Error
	if !errors.As(errExecute, &authErr) || authErr.Code != "auth_not_found" || authErr.Message != channelConnectionLimitMessage || authErr.HTTPStatus != 0 {
		t.Fatalf("Execute() error = %+v, want auth_not_found %q status 0", errExecute, channelConnectionLimitMessage)
	}
	current, ok := manager.GetByID("auth-a")
	if !ok || current == nil {
		t.Fatal("auth-a missing")
	}
	if current.Failed != 0 || current.Success != 0 || len(current.ModelStates) != 0 {
		t.Fatalf("auth state success=%d failed=%d states=%d, want untouched", current.Success, current.Failed, len(current.ModelStates))
	}

	close(executor.hold)
	if errWait := <-done; errWait != nil {
		t.Fatalf("holder Execute() error = %v", errWait)
	}
}

func TestChannelConnectionReusableAfterRelease(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := &channelGroupExecutor{}
	manager.RegisterExecutor(executor)
	model := "gpt-connection-reuse"
	registerConnectionAuth(t, manager, "auth-a", "10", 1, model)

	for i := 0; i < 2; i++ {
		resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		if errExecute != nil {
			t.Fatalf("Execute() #%d error = %v", i, errExecute)
		}
		if string(resp.Payload) != "ok" {
			t.Fatalf("payload = %q", resp.Payload)
		}
	}
	if manager.ActiveChannelConnections("auth-a") != 0 {
		t.Fatalf("active = %d, want 0", manager.ActiveChannelConnections("auth-a"))
	}
}

func TestChannelConnectionUnlimitedAllowsOverlap(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := newHoldingExecutor()
	manager.RegisterExecutor(executor)
	model := "gpt-connection-unlimited"
	registerConnectionAuth(t, manager, "auth-a", "10", 0, model)

	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			errCh <- errExecute
		}()
	}
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("first channel = %q, want auth-a", got)
	}
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("second channel = %q, want auth-a", got)
	}
	close(executor.hold)
	for i := 0; i < 2; i++ {
		if errExecute := <-errCh; errExecute != nil {
			t.Fatalf("Execute() error = %v", errExecute)
		}
	}
}

func TestChannelConnectionStreamHoldsUntilDrained(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := newHoldingExecutor()
	manager.RegisterExecutor(executor)
	model := "gpt-connection-stream"
	registerConnectionAuth(t, manager, "auth-a", "10", 1, model)

	result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("stream channel = %q, want auth-a", got)
	}
	if active := manager.ActiveChannelConnections("auth-a"); active != 1 {
		t.Fatalf("active while streaming = %d, want 1", active)
	}

	errBlocked := executeWithTimeout(t, manager, model)
	var authErr *Error
	if !errors.As(errBlocked, &authErr) || authErr.Code != "auth_not_found" || authErr.Message != channelConnectionLimitMessage || authErr.HTTPStatus != 0 {
		t.Fatalf("Execute() during stream error = %+v, want auth_not_found %q status 0", errBlocked, channelConnectionLimitMessage)
	}

	close(executor.hold)
	for range result.Chunks {
	}
	if active := manager.ActiveChannelConnections("auth-a"); active != 0 {
		t.Fatalf("active after drain = %d, want 0", active)
	}
}

type forwardBlockingExecutor struct {
	entered   chan string
	delivered chan struct{}
	hold      chan struct{}
	once      sync.Once
}

func newForwardBlockingExecutor() *forwardBlockingExecutor {
	return &forwardBlockingExecutor{
		entered:   make(chan string, 4),
		delivered: make(chan struct{}),
		hold:      make(chan struct{}),
	}
}

func (*forwardBlockingExecutor) Identifier() string { return "codex" }
func (*forwardBlockingExecutor) ShouldPrepareRequestAuth(*Auth) bool {
	return false
}
func (*forwardBlockingExecutor) PrepareRequestAuth(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}
func (e *forwardBlockingExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	e.entered <- id
	<-e.hold
	return cliproxyexecutor.Response{Payload: []byte(id)}, nil
}
func (e *forwardBlockingExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	chunks := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
		e.once.Do(func() { close(e.delivered) })
		<-e.hold
		close(chunks)
	}()
	e.entered <- id
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (*forwardBlockingExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}
func (e *forwardBlockingExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}
func (*forwardBlockingExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func (e *forwardBlockingExecutor) waitEntered(t *testing.T) string {
	t.Helper()
	select {
	case id := <-e.entered:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel execution")
	}
	return ""
}

func TestChannelConnectionStreamCancelReleasesWithoutRead(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := newForwardBlockingExecutor()
	manager.RegisterExecutor(executor)
	model := "gpt-connection-cancel"
	registerConnectionAuth(t, manager, "auth-a", "10", 1, model)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, errExecute := manager.ExecuteStream(ctx, []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("stream channel = %q, want auth-a", got)
	}
	select {
	case <-executor.delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the stream chunk to reach the forwarder")
	}
	if active := manager.ActiveChannelConnections("auth-a"); active != 1 {
		t.Fatalf("active before cancel = %d, want 1", active)
	}

	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for manager.ActiveChannelConnections("auth-a") != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("active after cancel = %d, want 0", manager.ActiveChannelConnections("auth-a"))
		}
		time.Sleep(10 * time.Millisecond)
	}

	done := make(chan error, 1)
	go func() {
		_, errAgain := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
		done <- errAgain
	}()
	if got := executor.waitEntered(t); got != "auth-a" {
		t.Fatalf("reacquired channel = %q, want auth-a", got)
	}
	close(executor.hold)
	if errAgain := <-done; errAgain != nil {
		t.Fatalf("Execute() after cancel error = %v", errAgain)
	}
	if result != nil {
		for range result.Chunks {
		}
	}
	if active := manager.ActiveChannelConnections("auth-a"); active != 0 {
		t.Fatalf("active after reacquire = %d, want 0", active)
	}
}

func TestMaxConcurrentConnectionsReadsHyphenatedMetadata(t *testing.T) {
	alias := &Auth{Metadata: map[string]any{"max-concurrent-connections": 4}}
	if got := alias.MaxConcurrentConnections(); got != 4 {
		t.Fatalf("alias limit = %d, want 4", got)
	}
	canonical := &Auth{Metadata: map[string]any{
		"max_concurrent_connections": 2,
		"max-concurrent-connections": 9,
	}}
	if got := canonical.MaxConcurrentConnections(); got != 2 {
		t.Fatalf("canonical limit = %d, want 2", got)
	}
	unlimited := &Auth{Metadata: map[string]any{"max_concurrent_connections": 0, "max-concurrent-connections": 9}}
	if got := unlimited.MaxConcurrentConnections(); got != 0 {
		t.Fatalf("explicit zero limit = %d, want 0", got)
	}
}

func TestTryAcquireChannelConnectionReleaseIsIdempotent(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	auth := &Auth{ID: "auth-a", Metadata: map[string]any{"max_concurrent_connections": 1}}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	release, ok := manager.tryAcquireChannelConnection(auth)
	if !ok {
		t.Fatal("first acquire failed")
	}
	if _, okAgain := manager.tryAcquireChannelConnection(auth); okAgain {
		t.Fatal("second acquire succeeded at limit 1")
	}
	release()
	release()
	if manager.ActiveChannelConnections("auth-a") != 0 {
		t.Fatalf("active after double release = %d, want 0", manager.ActiveChannelConnections("auth-a"))
	}
	if _, okAgain := manager.tryAcquireChannelConnection(auth); !okAgain {
		t.Fatal("acquire after release failed")
	}
}
