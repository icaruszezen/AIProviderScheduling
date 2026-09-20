package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	codexFakeSpaceDelta = `{"type":"response.output_text.delta","delta":" "}`
	codexFakeDashDelta  = `{"type":"response.output_text.delta","delta":"-"}`
	codexHelloDelta     = `{"type":"response.output_text.delta","delta":"hello"}`
	codexFailedEvent    = `{"type":"response.failed","response":{"error":{"type":"server_error","code":"upstream_failed","message":"upstream failed"}}}`
	codexBareErrorJSON  = `{"error":{"message":"upstream failed","type":"server_error"}}`
)

func codexFakeFirstTokenAuth(baseURL string, tokens ...string) *cliproxyauth.Auth {
	return &cliproxyauth.Auth{
		Provider:   "codex",
		Attributes: map[string]string{"base_url": baseURL, "api_key": "test"},
		Metadata:   map[string]any{"stream_fake_first_tokens": tokens}}
}

func TestCodexExecutor_FakeFirstToken_DropsProbeThenForwardsRealText(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexInProgressEvent, codexFakeSpaceDelta, codexHelloDelta, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " ", "-"), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	combined, streamErr := drainChunks(result)
	if streamErr != nil {
		t.Fatalf("unexpected chunk error: %v", streamErr)
	}
	if strings.Contains(combined, `"delta":" "`) {
		t.Fatalf("fake first token was forwarded: %s", combined)
	}
	createdAt := strings.Index(combined, "response.created")
	helloAt := strings.Index(combined, "hello")
	if createdAt < 0 || helloAt < 0 {
		t.Fatalf("missing handshake or real first token: %s", combined)
	}
	if createdAt > helloAt {
		t.Fatalf("handshake must be replayed before the real first token: %s", combined)
	}
}

func TestCodexExecutor_FakeFirstToken_ErrorAfterProbeFailsAttempt(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{name: "type error", events: []string{codexCreatedEvent, codexFakeSpaceDelta, codexInvalidEvent}},
		{name: "response failed", events: []string{codexCreatedEvent, codexFakeSpaceDelta, codexFailedEvent}},
		{name: "error object", events: []string{codexCreatedEvent, codexFakeSpaceDelta, codexBareErrorJSON}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := codexSSEServer(tt.events...)
			defer server.Close()

			req, opts := codexTestRequest()
			result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " "), req, opts)
			if err == nil {
				t.Fatal("expected ExecuteStream to fail the attempt after a fake first token")
			}
			if result != nil {
				t.Fatal("expected nil result so no handshake or fake token can reach the conductor")
			}
			if got := statusCodeFromTestError(t, err); got < 400 {
				t.Fatalf("status code = %d, want an HTTP error", got)
			}
		})
	}
}

func TestCodexExecutor_FakeFirstToken_BareJSONAfterProbeFailsAttempt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.created\ndata: " + codexCreatedEvent + "\n\n"))
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: " + codexFakeSpaceDelta + "\n\n"))
		_, _ = w.Write([]byte(codexBareErrorJSON + "\n"))
	}))
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " "), req, opts)
	if err == nil {
		t.Fatal("expected ExecuteStream to fail on a bare JSON error after a fake first token")
	}
	if result != nil {
		t.Fatal("expected nil result so the conductor can retry")
	}
}

func TestCodexExecutor_FakeFirstToken_EOFAfterProbeFailsAttempt(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexFakeSpaceDelta)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " "), req, opts)
	if err == nil {
		t.Fatal("expected ExecuteStream to fail when the stream ends after a fake first token")
	}
	if result != nil {
		t.Fatal("expected nil result so the conductor can retry")
	}
	if got := statusCodeFromTestError(t, err); got != http.StatusBadGateway {
		t.Fatalf("status code = %d, want %d", got, http.StatusBadGateway)
	}
}

func TestCodexExecutor_FakeFirstToken_DisabledPassesSpaceThrough(t *testing.T) {
	server := codexSSEServer(codexCreatedEvent, codexFakeSpaceDelta, codexHelloDelta, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	combined, streamErr := drainChunks(result)
	if streamErr != nil {
		t.Fatalf("unexpected chunk error: %v", streamErr)
	}
	if !strings.Contains(combined, `"delta":" "`) {
		t.Fatalf("unconfigured credential must forward a space first token: %s", combined)
	}
}

func TestCodexWebsocketsExecutor_FakeFirstToken_DropsProbeThenForwardsRealText(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexFakeSpaceDelta, codexFakeDashDelta, codexHelloDelta, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " ", "-"), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	combined, streamErr := drainChunks(result)
	if streamErr != nil {
		t.Fatalf("unexpected chunk error: %v", streamErr)
	}
	if strings.Contains(combined, `"delta":" "`) || strings.Contains(combined, `"delta":"-"`) {
		t.Fatalf("fake first tokens were forwarded: %s", combined)
	}
	if !strings.Contains(combined, "hello") {
		t.Fatalf("missing real first token: %s", combined)
	}
}

func TestCodexWebsocketsExecutor_FakeFirstToken_ErrorAfterProbeFailsAttempt(t *testing.T) {
	server := codexWebsocketServer(t, codexCreatedEvent, codexFakeSpaceDelta, codexFailedEvent)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(&config.Config{}).ExecuteStream(context.Background(), codexFakeFirstTokenAuth(server.URL, " "), req, opts)
	if err == nil {
		t.Fatal("expected ExecuteStream to fail the websocket attempt after a fake first token")
	}
	if result != nil {
		t.Fatal("expected nil result so no handshake or fake token can reach the conductor")
	}
}
