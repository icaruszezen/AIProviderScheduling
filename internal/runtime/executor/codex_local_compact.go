package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// codexLocalCompactSummaryPrompt asks the model to summarize the conversation itself.
// Third-party Codex endpoints answer 404 for /responses/compact, so local compaction is
// a normal Responses turn whose final user item carries this instruction.
const codexLocalCompactSummaryPrompt = `Your task is to produce a faithful, concise summary of the conversation so far so that a successor assistant can continue the work seamlessly after the earlier turns are discarded. The successor will see the user's original query plus this summary. Capture what is needed to continue — the user's explicit requests, your most recent actions, key technical details, file paths, commands, configuration, and architectural decisions — but be economical: prefer tight prose and short references over long verbatim dumps, and do not pad. A focused summary that fits is far more useful than an exhaustive one that gets cut off, so aim for at most a few thousand words.

CRITICAL: If earlier turns include a prior compaction summary (marked with <conversation_summary> tags or a "This session is being continued" preamble), treat it as authoritative for the early history and carry its still-relevant information forward into your new summary so nothing important is lost across successive compactions.

Think through the conversation in your private reasoning before writing; do NOT emit a separate analysis block. Output the final summary inside a single <summary>...</summary> block, organized into the following numbered sections. Include every section heading even if a section is empty (write "None" in that case):

1. Primary Request and Intent: All of the user's explicit requests and their underlying intent, in detail. Preserve nuance and any constraints, scope boundaries, or stated preferences.
2. Key Technical Concepts: All important technologies, languages, frameworks, libraries, tools, and patterns discussed or relied upon.
3. Files and Code Sections: Every file examined, created, or modified. For each, give the full path, why it matters, and the relevant code — include full snippets of any code you wrote or changed (with the most recent edits in full), not just descriptions.
4. Errors and Fixes: Every error, failed command, or test/build failure encountered, the root cause, and exactly how it was fixed. Note any fix that came from user feedback verbatim.
5. Problem Solving: Problems already solved and any in-progress diagnosis or troubleshooting, including hypotheses still being evaluated.
6. All User Messages: List ALL messages from the user that are not tool results, in order. These are critical for understanding intent and how it evolved. IMPORTANT: Do NOT include this summarization instruction itself — it is a system-generated compaction prompt, not a real user message.
7. Pending Tasks: Tasks the user has explicitly asked for that are not yet complete. Do not invent tasks the user never requested.
8. Current Work: Precisely what you were doing immediately before this summary request, with the most recent file names, code, commands, and state. Be specific enough that work can resume mid-stream.
9. Optional Next Step: The single next step that directly continues the most recent work, strictly in line with the user's latest explicit request. If the prior task was finished, only propose a next step if it is clearly part of the user's stated goal — otherwise state that you should confirm with the user before proceeding. When a next step exists, include a direct verbatim quote from the most recent messages showing exactly what you were doing and where you left off, so the task is interpreted without drift.

IMPORTANT: Do NOT call or use any tools. Respond with ONLY the <summary>...</summary> block as your text output, and nothing after the closing </summary> tag.`

const (
	// openAICompactModelSuffix is the compact-only model variant naming convention used by
	// upstream gateways. Local compaction always talks to the base model instead.
	openAICompactModelSuffix = "-openai-compact"

	// codexLocalCompactMetadataKey marks the summarization turn issued by executeLocalCompact
	// so the shared request pipeline skips tool injection for it.
	codexLocalCompactMetadataKey = "codex_local_compact"

	// codexLocalCompactBlobPrefix tags the opaque encrypted_content CPA synthesizes when the
	// upstream returns no encrypted reasoning. It lets the replay path tell CPA-owned blobs
	// apart from real upstream ones.
	codexLocalCompactBlobPrefix = "cpa-local-compaction."
)

// localCompactEnabled reports whether /v1/responses/compact should be answered locally.
// A credential-scoped override wins over the global switch.
func localCompactEnabled(cfg *config.Config, auth *cliproxyauth.Auth) bool {
	if value, ok := auth.LocalCompactOverride(); ok {
		return value
	}
	if cfg == nil {
		return false
	}
	return cfg.LocalCompact
}

// stripOpenAICompactModelSuffix maps a compact model variant back to its base model,
// e.g. "gpt-5.6-terra-openai-compact" becomes "gpt-5.6-terra".
func stripOpenAICompactModelSuffix(model string) string {
	stripped, ok := strings.CutSuffix(strings.TrimSpace(model), openAICompactModelSuffix)
	if !ok || stripped == "" {
		return model
	}
	return stripped
}

// codexLocalCompactActive reports whether the current execution is the summarization turn
// issued on behalf of a compact request.
func codexLocalCompactActive(opts cliproxyexecutor.Options) bool {
	if opts.Metadata == nil {
		return false
	}
	active, _ := opts.Metadata[codexLocalCompactMetadataKey].(bool)
	return active
}

// executeLocalCompact answers a compact request without touching the upstream
// /responses/compact endpoint: it runs a normal Responses turn asking the model to
// summarize, then reshapes the reply into a compaction response.
func (e *CodexExecutor) executeLocalCompact(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	clientModel := req.Model
	summaryReq, summaryOpts, err := prepareCodexLocalCompactExecution(req, opts)
	if err != nil {
		return cliproxyexecutor.Response{}, err
	}
	resp, err := e.Execute(ctx, auth, summaryReq, summaryOpts)
	if err != nil {
		return resp, err
	}
	payload, err := convertCodexResponseToCompaction(resp.Payload, clientModel)
	if err != nil {
		return resp, err
	}
	resp.Payload = payload
	return resp, nil
}

// prepareCodexLocalCompactExecution turns a compact request into the plain Responses
// request that carries the summarization prompt.
func prepareCodexLocalCompactExecution(req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Request, cliproxyexecutor.Options, error) {
	payload, err := buildCodexLocalCompactPayload(req.Payload)
	if err != nil {
		return req, opts, err
	}
	req.Payload = payload
	req.Model = stripOpenAICompactModelSuffix(req.Model)
	if len(opts.OriginalRequest) > 0 {
		original, errOriginal := buildCodexLocalCompactPayload(opts.OriginalRequest)
		if errOriginal != nil {
			return req, opts, errOriginal
		}
		opts.OriginalRequest = original
	}
	opts.Alt = ""
	opts.Stream = false

	// Copy the metadata so the marker and the rewritten model name stay local to this turn.
	metadata := make(map[string]any, len(opts.Metadata)+1)
	for key, value := range opts.Metadata {
		metadata[key] = value
	}
	if requested, ok := metadata[cliproxyexecutor.RequestedModelMetadataKey].(string); ok {
		metadata[cliproxyexecutor.RequestedModelMetadataKey] = stripOpenAICompactModelSuffix(requested)
	}
	metadata[codexLocalCompactMetadataKey] = true
	opts.Metadata = metadata
	return req, opts, nil
}

// buildCodexLocalCompactPayload appends the summarization prompt to the conversation and
// strips everything that only makes sense for a real compact call or a tool-using turn.
func buildCodexLocalCompactPayload(payload []byte) ([]byte, error) {
	if !gjson.ValidBytes(payload) {
		return nil, fmt.Errorf("codex local compact: request body is not valid JSON")
	}
	items, err := codexLocalCompactInputItems(gjson.GetBytes(payload, "input"))
	if err != nil {
		return nil, err
	}
	kept := make([]string, 0, len(items)+1)
	for _, item := range items {
		if strings.TrimSpace(gjson.Get(item, "type").String()) == "compaction_trigger" {
			continue
		}
		kept = append(kept, item)
	}
	kept = append(kept, codexLocalCompactUserMessage(codexLocalCompactSummaryPrompt))

	body, err := sjson.SetRawBytes(payload, "input", codexLocalCompactJSONArray(kept))
	if err != nil {
		return nil, fmt.Errorf("codex local compact: rebuild input: %w", err)
	}
	// The model must not answer with tool calls, so no tool wiring survives.
	body, _ = sjson.DeleteBytes(body, "tools")
	body, _ = sjson.DeleteBytes(body, "tool_choice")
	body, _ = sjson.DeleteBytes(body, "parallel_tool_calls")
	body, _ = sjson.DeleteBytes(body, "stream")
	body, err = sjson.SetBytes(body, "store", false)
	if err != nil {
		return nil, fmt.Errorf("codex local compact: set store: %w", err)
	}
	return body, nil
}

// codexLocalCompactInputItems normalizes the Responses input field into raw item JSON.
func codexLocalCompactInputItems(input gjson.Result) ([]string, error) {
	switch {
	case !input.Exists(), input.Type == gjson.Null:
		return nil, nil
	case input.IsArray():
		items := input.Array()
		raws := make([]string, 0, len(items))
		for _, item := range items {
			raws = append(raws, item.Raw)
		}
		return raws, nil
	case input.Type == gjson.String:
		return []string{codexLocalCompactUserMessage(input.String())}, nil
	case input.IsObject():
		return []string{input.Raw}, nil
	default:
		return nil, fmt.Errorf("codex local compact: input must be a string, object, or array")
	}
}

func codexLocalCompactUserMessage(text string) string {
	encoded, _ := json.Marshal(text)
	return `{"type":"message","role":"user","content":[{"type":"input_text","text":` + string(encoded) + `}]}`
}

func codexLocalCompactJSONArray(items []string) []byte {
	return []byte("[" + strings.Join(items, ",") + "]")
}

// convertCodexResponseToCompaction reshapes a completed Responses object into the
// response.compaction payload the Codex CLI expects back from a compact call.
func convertCodexResponseToCompaction(payload []byte, clientModel string) ([]byte, error) {
	if !gjson.ValidBytes(payload) {
		return nil, fmt.Errorf("codex local compact: upstream response is not valid JSON")
	}
	output := gjson.GetBytes(payload, "output")
	if !output.Exists() || !output.IsArray() {
		return nil, fmt.Errorf("codex local compact: upstream response carries no output array")
	}

	var encrypted string
	summaryParts := make([]string, 0, 2)
	for _, item := range output.Array() {
		switch strings.TrimSpace(item.Get("type").String()) {
		case "reasoning":
			if value := strings.TrimSpace(item.Get("encrypted_content").String()); value != "" {
				encrypted = value
			}
		case "message":
			for _, part := range item.Get("content").Array() {
				if text := strings.TrimSpace(part.Get("text").String()); text != "" {
					summaryParts = append(summaryParts, text)
				}
			}
		}
	}
	summary := strings.TrimSpace(strings.Join(summaryParts, "\n"))
	if summary == "" {
		return nil, fmt.Errorf("codex local compact: upstream response carries no summary text")
	}
	if encrypted == "" {
		// Third-party Codex endpoints routinely omit encrypted reasoning, yet a compaction
		// item without encrypted_content is rejected downstream. Stand in a CPA-owned blob
		// that the replay path can expand back into plain conversation context.
		encrypted = encodeCodexLocalCompactBlob(summary)
	}

	compactionItem, err := json.Marshal(map[string]any{
		"id":                "cmp_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		"type":              "compaction",
		"status":            "completed",
		"encrypted_content": encrypted,
		"summary": []any{map[string]any{
			"type": "summary_text",
			"text": summary,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("codex local compact: encode compaction item: %w", err)
	}

	out, err := sjson.SetRawBytes(payload, "output", codexLocalCompactJSONArray([]string{string(compactionItem)}))
	if err != nil {
		return nil, fmt.Errorf("codex local compact: rebuild output: %w", err)
	}
	if out, err = sjson.SetBytes(out, "object", "response.compaction"); err != nil {
		return nil, fmt.Errorf("codex local compact: set object: %w", err)
	}
	if out, err = sjson.SetBytes(out, "status", "completed"); err != nil {
		return nil, fmt.Errorf("codex local compact: set status: %w", err)
	}
	if trimmed := strings.TrimSpace(clientModel); trimmed != "" {
		if out, err = sjson.SetBytes(out, "model", trimmed); err != nil {
			return nil, fmt.Errorf("codex local compact: set model: %w", err)
		}
	}
	out, _ = sjson.DeleteBytes(out, "output_text")
	return out, nil
}

// restoreCodexCompactionInputItems rewrites compaction items replayed by the client into
// items a plain Responses upstream understands. Upstream-issued blobs go back as reasoning;
// CPA-synthesized ones become a plain conversation summary message.
func restoreCodexCompactionInputItems(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body
	}
	items := input.Array()
	found := false
	for _, item := range items {
		if isCodexCompactionItemType(item.Get("type").String()) {
			found = true
			break
		}
	}
	if !found {
		return body
	}

	restored := make([]string, 0, len(items)+1)
	for _, item := range items {
		if !isCodexCompactionItemType(item.Get("type").String()) {
			restored = append(restored, item.Raw)
			continue
		}
		summary := codexCompactionSummaryText(item)
		encrypted := strings.TrimSpace(item.Get("encrypted_content").String())
		if blobSummary, ours := decodeCodexLocalCompactBlob(encrypted); ours {
			if summary == "" {
				summary = blobSummary
			}
		} else if encrypted != "" {
			encoded, _ := json.Marshal(encrypted)
			restored = append(restored, `{"type":"reasoning","summary":[],"encrypted_content":`+string(encoded)+`}`)
		}
		if summary != "" {
			restored = append(restored, codexLocalCompactUserMessage("<conversation_summary>\n"+summary+"\n</conversation_summary>"))
		}
	}

	updated, err := sjson.SetRawBytes(body, "input", codexLocalCompactJSONArray(restored))
	if err != nil {
		return body
	}
	return updated
}

func isCodexCompactionItemType(value string) bool {
	switch strings.TrimSpace(value) {
	case "compaction", "compaction_summary":
		return true
	default:
		return false
	}
}

func codexCompactionSummaryText(item gjson.Result) string {
	summary := item.Get("summary")
	if summary.Type == gjson.String {
		return strings.TrimSpace(summary.String())
	}
	parts := make([]string, 0, 2)
	for _, part := range summary.Array() {
		if text := strings.TrimSpace(part.Get("text").String()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func encodeCodexLocalCompactBlob(summary string) string {
	doc, _ := json.Marshal(struct {
		Version int    `json:"cpa_local_compaction"`
		Summary string `json:"summary"`
	}{Version: 1, Summary: summary})
	return codexLocalCompactBlobPrefix + base64.RawURLEncoding.EncodeToString(doc)
}

// decodeCodexLocalCompactBlob reports whether the blob was synthesized by CPA and, when so,
// returns the summary it carries.
func decodeCodexLocalCompactBlob(blob string) (string, bool) {
	encoded, ours := strings.CutPrefix(strings.TrimSpace(blob), codexLocalCompactBlobPrefix)
	if !ours {
		return "", false
	}
	doc, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", true
	}
	return strings.TrimSpace(gjson.GetBytes(doc, "summary").String()), true
}
