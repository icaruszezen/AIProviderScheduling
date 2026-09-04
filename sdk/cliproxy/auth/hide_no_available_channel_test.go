package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type statusTextError struct {
	code int
	msg  string
}

func (e statusTextError) Error() string   { return e.msg }
func (e statusTextError) StatusCode() int { return e.code }

func TestIsNoAvailableChannelError(t *testing.T) {
	t.Parallel()

	channelBody := `{"error":{"message":"No available channel for model gpt-5.6-sol under group 浅梦号池促销 (distributor)"}}`
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typical new api 503",
			err:  statusTextError{code: http.StatusServiceUnavailable, msg: channelBody},
			want: true,
		},
		{
			name: "case insensitive",
			err:  statusTextError{code: http.StatusServiceUnavailable, msg: "NO AVAILABLE CHANNEL FOR MODEL foo"},
			want: true,
		},
		{
			name: "other 503",
			err:  statusTextError{code: http.StatusServiceUnavailable, msg: "upstream overloaded"},
			want: false,
		},
		{
			name: "channel text but not 503",
			err:  statusTextError{code: http.StatusBadGateway, msg: channelBody},
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoAvailableChannelError(tt.err); got != tt.want {
				t.Fatalf("IsNoAvailableChannelError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaybeMarkHideNoAvailableChannel(t *testing.T) {
	t.Parallel()

	errChannel := statusTextError{
		code: http.StatusServiceUnavailable,
		msg:  "No available channel for model gpt-5.6-sol",
	}
	enabled := &Auth{Metadata: map[string]any{"hide_no_available_channel": true}}
	disabled := &Auth{Metadata: map[string]any{}}

	marked := maybeMarkHideNoAvailableChannel(enabled, errChannel)
	if !HidesNoAvailableChannel(marked) {
		t.Fatal("expected marker when provider switch is on")
	}
	if marked.Error() != errChannel.Error() {
		t.Fatalf("Error() = %q, want original %q", marked.Error(), errChannel.Error())
	}

	unmarked := maybeMarkHideNoAvailableChannel(disabled, errChannel)
	if HidesNoAvailableChannel(unmarked) {
		t.Fatal("did not expect marker when provider switch is off")
	}

	other := maybeMarkHideNoAvailableChannel(enabled, statusTextError{code: http.StatusServiceUnavailable, msg: "busy"})
	if HidesNoAvailableChannel(other) {
		t.Fatal("did not expect marker for unrelated 503")
	}
}

func TestMarkHideNoAvailableChannelPreservesUnwrap(t *testing.T) {
	t.Parallel()

	base := errors.New("No available channel for model x")
	wrapped := MarkHideNoAvailableChannel(base)
	if !errors.Is(wrapped, base) {
		t.Fatal("marker should unwrap to the original error")
	}
	if !strings.Contains(wrapped.Error(), "No available channel for model") {
		t.Fatalf("Error() lost original text: %q", wrapped.Error())
	}
}

type hideChannelRetryAfterError struct {
	code       int
	msg        string
	retryAfter time.Duration
}

func (e hideChannelRetryAfterError) Error() string { return e.msg }
func (e hideChannelRetryAfterError) StatusCode() int {
	return e.code
}
func (e hideChannelRetryAfterError) RetryAfter() *time.Duration {
	value := e.retryAfter
	return &value
}

func TestMarkHideNoAvailableChannelForwardsStatusAndRetryAfter(t *testing.T) {
	t.Parallel()

	base := hideChannelRetryAfterError{
		code:       http.StatusServiceUnavailable,
		msg:        "No available channel for model x",
		retryAfter: 7 * time.Second,
	}
	wrapped := MarkHideNoAvailableChannel(base)
	if statusCodeFromError(wrapped) != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode() = %d, want %d", statusCodeFromError(wrapped), http.StatusServiceUnavailable)
	}
	got := retryAfterFromError(wrapped)
	if got == nil || *got != 7*time.Second {
		t.Fatalf("RetryAfter() = %v, want 7s", got)
	}
}

const homeHideChannelProvider = "home-hide-channel"

type homeHideChannelDispatcher struct {
	hide bool
}

func (homeHideChannelDispatcher) HeartbeatOK() bool { return true }

func (d homeHideChannelDispatcher) RPopAuth(context.Context, string, string, http.Header, int) ([]byte, error) {
	metadata := map[string]any{}
	if d.hide {
		metadata["hide_no_available_channel"] = true
	}
	return json.Marshal(homeAuthDispatchResponse{Auth: Auth{
		ID:       "home-hide-auth",
		Provider: homeHideChannelProvider,
		Status:   StatusActive,
		Metadata: metadata,
	}})
}

func (homeHideChannelDispatcher) AbortAmbiguousDispatch() {}

type homeHideChannelExecutor struct {
	countTokens bool
}

func (*homeHideChannelExecutor) Identifier() string { return homeHideChannelProvider }

func (e *homeHideChannelExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if e.countTokens {
		return cliproxyexecutor.Response{}, nil
	}
	return cliproxyexecutor.Response{}, statusTextError{
		code: http.StatusServiceUnavailable,
		msg:  "No available channel for model gpt-5.6-sol",
	}
}

func (*homeHideChannelExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (*homeHideChannelExecutor) Refresh(context.Context, *Auth) (*Auth, error) { return nil, nil }

func (e *homeHideChannelExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if !e.countTokens {
		return cliproxyexecutor.Response{}, nil
	}
	return cliproxyexecutor.Response{}, statusTextError{
		code: http.StatusServiceUnavailable,
		msg:  "No available channel for model gpt-5.6-sol",
	}
}

func (*homeHideChannelExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestHomeExecuteMarksHideNoAvailableChannel(t *testing.T) {
	tests := []struct {
		name        string
		hide        bool
		countTokens bool
		wantMarked  bool
	}{
		{name: "execute marked", hide: true, wantMarked: true},
		{name: "execute unmarked", hide: false, wantMarked: false},
		{name: "count tokens marked", hide: true, countTokens: true, wantMarked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
			manager.PublishHomeDispatch(homeHideChannelDispatcher{hide: tt.hide}, executionregistry.New(), 1)
			manager.RegisterExecutor(&homeHideChannelExecutor{countTokens: tt.countTokens})

			var errExec error
			if tt.countTokens {
				_, errExec = manager.ExecuteCount(context.Background(), []string{homeHideChannelProvider}, cliproxyexecutor.Request{Model: "test"}, cliproxyexecutor.Options{})
			} else {
				_, errExec = manager.Execute(context.Background(), []string{homeHideChannelProvider}, cliproxyexecutor.Request{Model: "test"}, cliproxyexecutor.Options{})
			}
			if errExec == nil {
				t.Fatal("expected upstream channel error")
			}
			if !strings.Contains(errExec.Error(), "No available channel for model") {
				t.Fatalf("Error() = %q, want original channel text", errExec)
			}
			if got := HidesNoAvailableChannel(errExec); got != tt.wantMarked {
				t.Fatalf("HidesNoAvailableChannel() = %v, want %v", got, tt.wantMarked)
			}
		})
	}
}
