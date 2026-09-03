package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func codexLocalCompactSSE(t *testing.T, output string) string {
	t.Helper()
	return "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_local\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":" +
		output +
		",\"usage\":{\"input_tokens\":11,\"output_tokens\":22,\"total_tokens\":33}}}\n\n"
}

// newCodexLocalCompactUpstream serves the summarization turn and records what CPA sent.
func newCodexLocalCompactUpstream(t *testing.T, output string, gotPath *string, gotBody *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		*gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(codexLocalCompactSSE(t, output)))
	}))
}

func TestCodexExecutorLocalCompactRunsPlainResponsesTurn(t *testing.T) {
	const upstreamOutput = `[{"type":"reasoning","summary":[],"encrypted_content":"upstream-reasoning-blob"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"<summary>session recap</summary>"}]}]`

	var gotPath string
	var gotBody []byte
	server := newCodexLocalCompactUpstream(t, upstreamOutput, &gotPath, &gotBody)
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{LocalCompact: true}})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}

	payload := `{"model":"gpt-5.4-openai-compact","input":[{"type":"message","role":"user","content":"history"},{"type":"compaction_trigger"}],` +
		`"tools":[{"type":"function","name":"shell"}],"tool_choice":"auto"}`
	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4-openai-compact",
		Payload: []byte(payload),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Alt:          "responses/compact",
		Stream:       false,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if gotPath != "/responses" {
		t.Fatalf("path = %q, want %q", gotPath, "/responses")
	}
	if model := gjson.GetBytes(gotBody, "model").String(); model != "gpt-5.4" {
		t.Fatalf("upstream model = %q, want %q; body=%s", model, "gpt-5.4", gotBody)
	}
	if gjson.GetBytes(gotBody, "tools").Exists() {
		t.Fatalf("summarization turn kept tools: %s", gotBody)
	}
	if gjson.GetBytes(gotBody, "tool_choice").Exists() {
		t.Fatalf("summarization turn kept an orphan tool_choice: %s", gotBody)
	}
	if store := gjson.GetBytes(gotBody, "store"); store.Type != gjson.False {
		t.Fatalf("store = %s, want false; body=%s", store.Raw, gotBody)
	}
	input := gjson.GetBytes(gotBody, "input").Array()
	if len(input) != 2 {
		t.Fatalf("input length = %d, want 2; body=%s", len(input), gotBody)
	}
	for _, item := range input {
		if item.Get("type").String() == "compaction_trigger" {
			t.Fatalf("summarization turn kept compaction_trigger: %s", gotBody)
		}
	}
	last := input[len(input)-1]
	if role := last.Get("role").String(); role != "user" {
		t.Fatalf("last input role = %q, want user; body=%s", role, gotBody)
	}
	if text := last.Get("content.0.text").String(); text != codexLocalCompactSummaryPrompt {
		t.Fatalf("last input item is not the summary prompt; body=%s", gotBody)
	}

	if object := gjson.GetBytes(resp.Payload, "object").String(); object != "response.compaction" {
		t.Fatalf("object = %q, want response.compaction; payload=%s", object, resp.Payload)
	}
	if status := gjson.GetBytes(resp.Payload, "status").String(); status != "completed" {
		t.Fatalf("status = %q, want completed; payload=%s", status, resp.Payload)
	}
	if model := gjson.GetBytes(resp.Payload, "model").String(); model != "gpt-5.4-openai-compact" {
		t.Fatalf("model = %q, want the client model; payload=%s", model, resp.Payload)
	}
	output := gjson.GetBytes(resp.Payload, "output").Array()
	if len(output) != 1 {
		t.Fatalf("output length = %d, want 1; payload=%s", len(output), resp.Payload)
	}
	item := output[0]
	if itemType := item.Get("type").String(); itemType != "compaction" {
		t.Fatalf("output[0].type = %q, want compaction; payload=%s", itemType, resp.Payload)
	}
	if item.Get("status").String() != "completed" {
		t.Fatalf("output[0].status = %q, want completed; payload=%s", item.Get("status").String(), resp.Payload)
	}
	if !strings.HasPrefix(item.Get("id").String(), "cmp_") {
		t.Fatalf("output[0].id = %q, want a cmp_ prefix; payload=%s", item.Get("id").String(), resp.Payload)
	}
	if encrypted := item.Get("encrypted_content").String(); encrypted != "upstream-reasoning-blob" {
		t.Fatalf("encrypted_content = %q, want the upstream blob; payload=%s", encrypted, resp.Payload)
	}
	if text := item.Get("summary.0.text").String(); text != "<summary>session recap</summary>" {
		t.Fatalf("summary text = %q; payload=%s", text, resp.Payload)
	}
	if item.Get("summary.0.type").String() != "summary_text" {
		t.Fatalf("summary type = %q, want summary_text; payload=%s", item.Get("summary.0.type").String(), resp.Payload)
	}
}

func TestCodexExecutorLocalCompactSynthesizesBlobWithoutUpstreamReasoning(t *testing.T) {
	const upstreamOutput = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"recap only"}]}]`

	var gotPath string
	var gotBody []byte
	server := newCodexLocalCompactUpstream(t, upstreamOutput, &gotPath, &gotBody)
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{LocalCompact: true}})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}

	resp, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"history"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Alt:          "responses/compact",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	encrypted := gjson.GetBytes(resp.Payload, "output.0.encrypted_content").String()
	if encrypted == "" {
		t.Fatalf("encrypted_content is empty; payload=%s", resp.Payload)
	}
	summary, ours := decodeCodexLocalCompactBlob(encrypted)
	if !ours {
		t.Fatalf("encrypted_content = %q, want a CPA-synthesized blob", encrypted)
	}
	if summary != "recap only" {
		t.Fatalf("blob summary = %q, want %q", summary, "recap only")
	}
}

func TestCodexExecutorCompactStaysPassthroughWhenCredentialOptsOut(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response.compaction"}`))
	}))
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{LocalCompact: true}})
	auth := &cliproxyauth.Auth{
		Attributes: map[string]string{"base_url": server.URL, "api_key": "test"},
		Metadata:   map[string]any{"local_compact": false},
	}

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":"history"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
		Alt:          "responses/compact",
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotPath != "/responses/compact" {
		t.Fatalf("path = %q, want %q", gotPath, "/responses/compact")
	}
}

func TestCodexExecutorReplaysCompactionItemAsConversationSummary(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(codexLocalCompactSSE(t, `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]`)))
	}))
	defer server.Close()

	executor := NewCodexExecutor(&config.Config{SDKConfig: config.SDKConfig{LocalCompact: true}})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}

	blob, errBlob := json.Marshal(encodeCodexLocalCompactBlob("earlier recap"))
	if errBlob != nil {
		t.Fatalf("marshal blob: %v", errBlob)
	}
	payload := `{"model":"gpt-5.4","input":[` +
		`{"type":"compaction","id":"cmp_1","encrypted_content":` + string(blob) + `,"summary":[{"type":"summary_text","text":"earlier recap"}]},` +
		`{"type":"message","role":"user","content":"next question"}]}`

	if _, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: []byte(payload),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai-response"),
	}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	input := gjson.GetBytes(gotBody, "input").Array()
	if len(input) != 2 {
		t.Fatalf("input length = %d, want 2; body=%s", len(input), gotBody)
	}
	for _, item := range input {
		if isCodexCompactionItemType(item.Get("type").String()) {
			t.Fatalf("compaction item reached the upstream: %s", gotBody)
		}
	}
	summaryText := input[0].Get("content.0.text").String()
	if !strings.Contains(summaryText, "<conversation_summary>") || !strings.Contains(summaryText, "earlier recap") {
		t.Fatalf("first input item is not the restored summary: %s", gotBody)
	}
}

func TestRestoreCodexCompactionInputItems(t *testing.T) {
	t.Run("upstream blob becomes a reasoning item", func(t *testing.T) {
		body := []byte(`{"input":[{"type":"compaction","encrypted_content":"upstream-blob","summary":[{"type":"summary_text","text":"recap"}]}]}`)
		items := gjson.GetBytes(restoreCodexCompactionInputItems(body), "input").Array()
		if len(items) != 2 {
			t.Fatalf("input length = %d, want 2", len(items))
		}
		if items[0].Get("type").String() != "reasoning" {
			t.Fatalf("items[0].type = %q, want reasoning", items[0].Get("type").String())
		}
		if items[0].Get("encrypted_content").String() != "upstream-blob" {
			t.Fatalf("items[0].encrypted_content = %q, want the upstream blob", items[0].Get("encrypted_content").String())
		}
		if text := items[1].Get("content.0.text").String(); text != "<conversation_summary>\nrecap\n</conversation_summary>" {
			t.Fatalf("items[1] text = %q", text)
		}
	})

	t.Run("synthetic blob becomes a summary message only", func(t *testing.T) {
		blob, _ := json.Marshal(encodeCodexLocalCompactBlob("recap"))
		body := []byte(`{"input":[{"type":"compaction_summary","encrypted_content":` + string(blob) + `}]}`)
		items := gjson.GetBytes(restoreCodexCompactionInputItems(body), "input").Array()
		if len(items) != 1 {
			t.Fatalf("input length = %d, want 1", len(items))
		}
		if text := items[0].Get("content.0.text").String(); text != "<conversation_summary>\nrecap\n</conversation_summary>" {
			t.Fatalf("items[0] text = %q", text)
		}
	})

	t.Run("bodies without compaction items are untouched", func(t *testing.T) {
		body := []byte(`{"input":[{"type":"message","role":"user","content":"hi"}]}`)
		if got := restoreCodexCompactionInputItems(body); string(got) != string(body) {
			t.Fatalf("body = %s, want it unchanged", got)
		}
	})
}

func TestStripOpenAICompactModelSuffix(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{model: "gpt-5.6-terra-openai-compact", want: "gpt-5.6-terra"},
		{model: "teamA/gpt-5.6-terra-openai-compact", want: "teamA/gpt-5.6-terra"},
		{model: "gpt-5.6-terra", want: "gpt-5.6-terra"},
		{model: "-openai-compact", want: "-openai-compact"},
		{model: "", want: ""},
	}
	for _, tc := range cases {
		if got := stripOpenAICompactModelSuffix(tc.model); got != tc.want {
			t.Errorf("stripOpenAICompactModelSuffix(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

func TestLocalCompactEnabled(t *testing.T) {
	cases := []struct {
		name   string
		global bool
		auth   *cliproxyauth.Auth
		want   bool
	}{
		{name: "off by default", auth: &cliproxyauth.Auth{}},
		{name: "global on", global: true, auth: &cliproxyauth.Auth{}, want: true},
		{name: "credential opts in", auth: &cliproxyauth.Auth{Metadata: map[string]any{"local_compact": true}}, want: true},
		{name: "credential opts out", global: true, auth: &cliproxyauth.Auth{Metadata: map[string]any{"local_compact": false}}},
		{name: "legacy key", auth: &cliproxyauth.Auth{Metadata: map[string]any{"local-compact": "true"}}, want: true},
		{name: "nil auth follows global", global: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{SDKConfig: config.SDKConfig{LocalCompact: tc.global}}
			if got := localCompactEnabled(cfg, tc.auth); got != tc.want {
				t.Fatalf("localCompactEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
