package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestChannelGroupPolicyMatchesStatusOrText(t *testing.T) {
	policy := channelGroupPolicyFromMetadata(map[string]any{
		ChannelGroupProviderMetadataKey: "codex",
		ChannelGroupMetadataKey:         "team",
		ChannelGroupPolicyMetadataKey:   `{"count":2,"status-codes":[429],"error-contains":["overloaded"]}`,
	})
	if !policy.scoped || policy.count != 2 {
		t.Fatalf("policy = %#v", policy)
	}
	if !policy.matches(&Error{HTTPStatus: http.StatusTooManyRequests, Message: "busy"}) {
		t.Fatal("expected status match")
	}
	if !policy.matches(&Error{HTTPStatus: http.StatusBadGateway, Message: "upstream overloaded"}) {
		t.Fatal("expected text match")
	}
	if policy.matches(&Error{HTTPStatus: http.StatusBadGateway, Message: "Overloaded"}) {
		t.Fatal("text match should be case sensitive")
	}
	if policy.matches(statusBodyError{status: http.StatusBadRequest, body: []byte(`{"error":"overloaded"}`)}) == false {
		t.Fatal("expected response body match")
	}
	empty := channelGroupPolicyFromMetadata(map[string]any{
		ChannelGroupProviderMetadataKey: "codex",
		ChannelGroupMetadataKey:         "team",
		ChannelGroupPolicyMetadataKey:   `{"count":2}`,
	})
	if empty.matches(&Error{HTTPStatus: http.StatusTooManyRequests, Message: "overloaded"}) {
		t.Fatal("empty matchers should not switch")
	}
}

type statusBodyError struct {
	status int
	body   []byte
}

func (e statusBodyError) Error() string        { return "upstream failed" }
func (e statusBodyError) StatusCode() int      { return e.status }
func (e statusBodyError) ResponseBody() []byte { return e.body }

type channelGroupExecutor struct {
	mu      sync.Mutex
	ids     []string
	failFor map[string]int
	status  int
	message string
}

func (*channelGroupExecutor) Identifier() string { return "codex" }
func (*channelGroupExecutor) ShouldPrepareRequestAuth(*Auth) bool {
	return false
}
func (*channelGroupExecutor) PrepareRequestAuth(context.Context, *Auth) (*Auth, error) {
	return nil, nil
}
func (e *channelGroupExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	id := ""
	if auth != nil {
		id = auth.ID
	}
	e.mu.Lock()
	e.ids = append(e.ids, id)
	remaining := 0
	if e.failFor != nil {
		remaining = e.failFor[id]
		if remaining > 0 {
			e.failFor[id] = remaining - 1
		}
	}
	e.mu.Unlock()
	if remaining > 0 {
		message := e.message
		if message == "" {
			message = "upstream failed"
		}
		return cliproxyexecutor.Response{}, &Error{HTTPStatus: e.status, Message: message}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}
func (e *channelGroupExecutor) ExecuteStream(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	_, err := e.Execute(ctx, auth, req, opts)
	if err != nil {
		return nil, err
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("ok")}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}
func (*channelGroupExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}
func (e *channelGroupExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}
func (*channelGroupExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{HTTPStatus: http.StatusNotImplemented, Message: "not implemented"}
}

func (e *channelGroupExecutor) calledIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.ids))
	copy(out, e.ids)
	return out
}

func TestExecuteChannelGroupStaysInsideGroup(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := &channelGroupExecutor{failFor: map[string]int{}}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-scope"
	registerChannelGroupAuth(t, manager, "outside", "other", "100", nil, model)
	registerChannelGroupAuth(t, manager, "inside", "team", "1", nil, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{500}, nil))
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q", resp.Payload)
	}
	if got := executor.calledIDs(); len(got) != 1 || got[0] != "inside" {
		t.Fatalf("calls = %#v, want only inside", got)
	}
}

func TestExecuteChannelGroupSameCredentialRetryDoesNotSwitch(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 100, "auth-b": 100},
		status:  http.StatusTooManyRequests,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-same"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", map[string]any{
		"provider_retry_count": 2,
		"request_retry":        5,
	}, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(0, []int{http.StatusTooManyRequests}, nil)); errExecute == nil {
		t.Fatal("expected the first channel to fail without switching")
	}
	if got := executor.calledIDs(); len(got) != 3 || got[0] != "auth-a" || got[1] != "auth-a" || got[2] != "auth-a" {
		t.Fatalf("calls = %#v, want 3 attempts on auth-a", got)
	}
}

func TestExecuteChannelGroupSwitchesOnStatusThenStops(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 100, "auth-b": 100, "auth-c": 100},
		status:  http.StatusBadGateway,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-switch"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "30", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "20", nil, model)
	registerChannelGroupAuth(t, manager, "auth-c", "team", "10", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{http.StatusBadGateway}, nil)); errExecute == nil {
		t.Fatal("expected failure after one switch")
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "auth-a" || got[1] != "auth-b" {
		t.Fatalf("calls = %#v, want auth-a then auth-b", got)
	}
}

func TestExecuteChannelGroupDoesNotSwitchOnUnmatchedError(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 100, "auth-b": 100},
		status:  http.StatusInternalServerError,
		message: "plain failure",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-nomatch"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(3, []int{http.StatusTooManyRequests}, []string{"overloaded"})); errExecute == nil {
		t.Fatal("expected unmatched error to stop")
	}
	if got := executor.calledIDs(); len(got) != 1 || got[0] != "auth-a" {
		t.Fatalf("calls = %#v, want only auth-a", got)
	}
}

func TestExecuteChannelGroupSwitchesOnListedRequestFault(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusBadRequest,
		message: "bad request",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-request-fault"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{http.StatusBadRequest}, nil))
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q", resp.Payload)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "auth-a" || got[1] != "auth-b" {
		t.Fatalf("calls = %#v, want auth-a then auth-b", got)
	}
}

func TestExecuteChannelGroupKeepsUnlistedRequestFault(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 1, "auth-b": 1},
		status:  http.StatusBadRequest,
		message: "bad request",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-unlisted-fault"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{http.StatusTooManyRequests}, nil)); errExecute == nil {
		t.Fatal("expected unlisted request fault to stop")
	}
	if got := executor.calledIDs(); len(got) != 1 || got[0] != "auth-a" {
		t.Fatalf("calls = %#v, want only auth-a", got)
	}
}

func TestExecuteStreamChannelGroupSwitchesOnListedRequestFault(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusBadRequest,
		message: "bad request",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-stream-fault"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	result, errExecute := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{http.StatusBadRequest}, nil))
	if errExecute != nil {
		t.Fatalf("ExecuteStream() error = %v", errExecute)
	}
	drainChannelGroupStream(t, result)
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "auth-a" || got[1] != "auth-b" {
		t.Fatalf("calls = %#v, want auth-a then auth-b", got)
	}
}

func TestExecuteChannelGroupSwitchesOnListedRequestScopedStop(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusBadRequest,
		message: "bad request",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-scoped-stop"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", map[string]any{
		"request_scoped_errors": []internalconfig.RequestScopedErrorRule{{
			Status: http.StatusBadRequest,
			Match:  []string{"bad request"},
			Action: "stop",
		}},
	}, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, []int{http.StatusBadRequest}, nil))
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q", resp.Payload)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "auth-a" || got[1] != "auth-b" {
		t.Fatalf("calls = %#v, want auth-a then auth-b", got)
	}
}

func drainChannelGroupStream(t *testing.T, result *cliproxyexecutor.StreamResult) {
	t.Helper()
	if result == nil || result.Chunks == nil {
		t.Fatal("expected stream chunks")
	}
	for range result.Chunks {
	}
}

func TestExecuteChannelGroupSwitchesOnErrorText(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"auth-a": 1},
		status:  http.StatusBadGateway,
		message: "capacity overloaded",
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-text"
	registerChannelGroupAuth(t, manager, "auth-a", "team", "10", nil, model)
	registerChannelGroupAuth(t, manager, "auth-b", "team", "1", nil, model)

	resp, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, channelGroupOptions(1, nil, []string{"overloaded"}))
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("payload = %q", resp.Payload)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "auth-a" || got[1] != "auth-b" {
		t.Fatalf("calls = %#v, want auth-a then auth-b", got)
	}
}

func TestExecuteWithoutChannelGroupUsesGlobalOrder(t *testing.T) {
	manager := NewManager(nil, providerRetrySelector{}, nil)
	executor := &channelGroupExecutor{
		failFor: map[string]int{"outside": 1},
		status:  http.StatusBadGateway,
	}
	manager.RegisterExecutor(executor)
	model := "gpt-channel-group-global"
	registerChannelGroupAuth(t, manager, "outside", "other", "100", nil, model)
	registerChannelGroupAuth(t, manager, "inside", "team", "1", nil, model)

	if _, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if got := executor.calledIDs(); len(got) != 2 || got[0] != "outside" || got[1] != "inside" {
		t.Fatalf("calls = %#v, want global priority failover", got)
	}
}

func channelGroupOptions(count int, codes []int, phrases []string) cliproxyexecutor.Options {
	raw, errMarshal := json.Marshal(channelGroupPolicyDocument{Count: count, StatusCodes: codes, ErrorContains: phrases})
	if errMarshal != nil {
		panic(errMarshal)
	}
	return cliproxyexecutor.Options{Metadata: map[string]any{
		ChannelGroupProviderMetadataKey: "codex",
		ChannelGroupMetadataKey:         "team",
		ChannelGroupPolicyMetadataKey:   string(raw),
	}}
}

func registerChannelGroupAuth(t *testing.T, manager *Manager, id, group, priority string, metadata map[string]any, model string) {
	t.Helper()
	if _, errRegister := manager.Register(context.Background(), &Auth{
		ID:       id,
		Provider: "codex",
		Status:   StatusActive,
		Attributes: map[string]string{
			AttributeChannelGroup: group,
			AttributeChannelPanel: "codex",
			"priority":            priority,
		},
		Metadata: metadata,
	}); errRegister != nil {
		t.Fatalf("Register(%s) error = %v", id, errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
}
