package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
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

// codexSSEServerPauseBefore flushes the leading events, then waits before writing the rest.
func codexSSEServerPauseBefore(pause <-chan struct{}, before []string, after ...string) *httptest.Server {
	writeEvents := func(w http.ResponseWriter, events []string) {
		for _, event := range events {
			eventType := "message"
			if parsed := strings.SplitN(event, `"type":"`, 2); len(parsed) == 2 {
				eventType = strings.SplitN(parsed[1], `"`, 2)[0]
			}
			_, _ = w.Write([]byte("event: " + eventType + "\n"))
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeEvents(w, before)
		<-pause
		writeEvents(w, after)
	}))
}

// readUntilContains reads stream chunks until combined contains needle, or the deadline fires.
func readUntilContains(t *testing.T, chunks <-chan cliproxyexecutor.StreamChunk, needle string) string {
	t.Helper()
	var combined strings.Builder
	deadline := time.After(2 * time.Second)
	for !strings.Contains(combined.String(), needle) {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("stream closed before %q: %s", needle, combined.String())
			}
			if chunk.Err != nil {
				t.Fatalf("chunk error before %q: %v; so far: %s", needle, chunk.Err, combined.String())
			}
			combined.Write(chunk.Payload)
		case <-deadline:
			t.Fatalf("timed out waiting for %q; so far: %s", needle, combined.String())
		}
	}
	return combined.String()
}

func TestCodexExecutor_FakeFirstToken_DisabledForwardsSpaceBeforeRealText(t *testing.T) {
	release := make(chan struct{})
	server := codexSSEServerPauseBefore(release, []string{codexFakeSpaceDelta}, codexHelloDelta, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexTestRequest()
	result, err := NewCodexExecutor(&config.Config{}).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	got := readUntilContains(t, result.Chunks, `"delta":" "`)
	if strings.Contains(got, "hello") {
		t.Fatalf("real text was forwarded before the upstream released it: %s", got)
	}
	close(release)
	rest := readUntilContains(t, result.Chunks, "hello")
	if !strings.Contains(rest, "hello") {
		t.Fatalf("missing real text after release: %s", rest)
	}
}

func codexWebsocketServerPauseBefore(t *testing.T, pause <-chan struct{}, before []string, after ...string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, errRead := conn.ReadMessage(); errRead != nil {
			t.Errorf("read websocket message: %v", errRead)
			return
		}
		for _, frame := range before {
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(frame)); errWrite != nil {
				t.Errorf("write leading frame: %v", errWrite)
				return
			}
		}
		<-pause
		for _, frame := range after {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		}
	}))
}

func TestCodexWebsocketsExecutor_FakeFirstToken_DisabledForwardsSpaceBeforeRealText(t *testing.T) {
	release := make(chan struct{})
	server := codexWebsocketServerPauseBefore(t, release, []string{codexFakeSpaceDelta}, codexHelloDelta, codexCompletedEventBody)
	defer server.Close()

	req, opts := codexWebsocketRequest()
	result, err := NewCodexWebsocketsExecutor(&config.Config{}).ExecuteStream(context.Background(), codexTestAuth(server.URL), req, opts)
	if err != nil {
		t.Fatalf("unexpected ExecuteStream error: %v", err)
	}
	got := readUntilContains(t, result.Chunks, `"delta":" "`)
	if strings.Contains(got, "hello") {
		t.Fatalf("real text was forwarded before the upstream released it: %s", got)
	}
	close(release)
	rest := readUntilContains(t, result.Chunks, "hello")
	if !strings.Contains(rest, "hello") {
		t.Fatalf("missing real text after release: %s", rest)
	}
}

func TestCodexExecutor_FakeFirstToken_DisabledHighConcurrencyKeepsSpaceTTFT(t *testing.T) {
	const (
		concurrency = 300
		realDelay   = 800 * time.Millisecond
		maxP99      = 200 * time.Millisecond
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: " + codexFakeSpaceDelta + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		timer := time.NewTimer(realDelay)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: " + codexHelloDelta + "\n\n"))
		_, _ = w.Write([]byte("event: response.completed\ndata: " + codexCompletedEventBody + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{})
	auth := codexTestAuth(server.URL)
	req, opts := codexTestRequest()
	latencies := make([]time.Duration, concurrency)
	errs := make([]error, concurrency)
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(index int) {
			defer wg.Done()
			start.Wait()
			// Spread the dials by a couple of milliseconds so a local listener
			// does not refuse the SYN burst, while every stream still overlaps
			// the real-text delay.
			time.Sleep(time.Duration(index) * 2 * time.Millisecond)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			begun := time.Now()
			result, err := executor.ExecuteStream(ctx, auth, req, opts)
			if err != nil {
				errs[index] = err
				return
			}
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					errs[index] = chunk.Err
					return
				}
				if strings.Contains(string(chunk.Payload), `"delta":" "`) {
					latencies[index] = time.Since(begun)
					cancel()
					return
				}
				if strings.Contains(string(chunk.Payload), "hello") {
					latencies[index] = time.Since(begun)
					errs[index] = errSpaceAfterRealText
					cancel()
					return
				}
			}
			errs[index] = errSpaceMissing
		}(i)
	}
	start.Done()
	wg.Wait()

	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
			if failed == 1 {
				t.Errorf("first stream error: %v", err)
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d/%d streams failed", failed, concurrency)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[concurrency/2]
	p99 := latencies[concurrency*99/100]
	slowest := latencies[concurrency-1]
	t.Logf("concurrency=%d fake-token TTFT p50=%s p99=%s max=%s (real text delayed %s)", concurrency, p50, p99, slowest, realDelay)
	if p99 >= realDelay/2 {
		t.Fatalf("p99 fake-token TTFT = %s, want well below the %s real-text delay", p99, realDelay)
	}
	if p99 > maxP99 {
		t.Fatalf("p99 fake-token TTFT = %s, want <= %s", p99, maxP99)
	}
}

var (
	errSpaceAfterRealText = errString("space token arrived with or after real text")
	errSpaceMissing       = errString("stream ended before the space token")
)

type errString string

func (e errString) Error() string { return string(e) }

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
