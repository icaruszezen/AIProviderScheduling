package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// ErrStreamFirstTokenTimeout is the cause used when a channel's first-token
// wait cancels the upstream attempt. ctx.Err() stays context.Canceled; callers
// distinguish this cancel with errors.Is(context.Cause(ctx), ErrStreamFirstTokenTimeout).
var ErrStreamFirstTokenTimeout = errors.New("stream first token timeout")

// StreamFirstTokenTimeoutError is returned to the scheduler so the attempt can
// fail over. It must not unwrap to context.Canceled.
type StreamFirstTokenTimeoutError struct {
	Timeout time.Duration
}

func (e *StreamFirstTokenTimeoutError) Error() string {
	if e == nil || e.Timeout <= 0 {
		return "stream first token timeout"
	}
	return fmt.Sprintf("stream first token timeout after %s", e.Timeout)
}

func (e *StreamFirstTokenTimeoutError) Unwrap() error { return ErrStreamFirstTokenTimeout }

func (e *StreamFirstTokenTimeoutError) StatusCode() int { return http.StatusGatewayTimeout }

// StreamFirstTokenTimeoutCanceled reports whether ctx was canceled because the
// first token did not arrive in time.
func StreamFirstTokenTimeoutCanceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), ErrStreamFirstTokenTimeout)
}

// IsStreamTokenPayload reports whether payload is the first substantive token
// for format. Formats without a classifier treat any non-empty payload as a token.
// Pure terminal markers do not count: the wait continues until real output, an
// in-stream error, stream closure, or the channel timeout.
func IsStreamTokenPayload(format sdktranslator.Format, payload []byte) bool {
	switch format {
	case sdktranslator.FormatClaude:
		return claudeTokenEvent(payload, true)
	case sdktranslator.FormatGemini, sdktranslator.FormatAntigravity:
		return geminiTokenEvent(payload, true)
	case sdktranslator.FormatInteractions:
		return IsInteractionsTokenEvent(payload)
	case sdktranslator.FormatOpenAI:
		return chatTokenEvent(payload, true)
	case sdktranslator.FormatOpenAIResponse, sdktranslator.FormatCodex:
		return responsesTokenEvent(payload, true)
	default:
		return len(bytes.TrimSpace(payload)) > 0
	}
}

// IsClaudeTokenEvent reports whether an Anthropic Messages SSE chunk carries
// substantive output token content.
func IsClaudeTokenEvent(payload []byte) bool {
	return claudeTokenEvent(payload, false)
}

func claudeTokenEvent(payload []byte, substantive bool) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}
	if bytes.HasPrefix(payload, []byte("event:")) {
		if idx := bytes.IndexByte(payload, '\n'); idx != -1 {
			payload = bytes.TrimSpace(payload[idx+1:])
		}
	}
	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
		if len(payload) == 0 {
			return false
		}
	}
	eventType := gjson.GetBytes(payload, "type").String()
	switch eventType {
	case "content_block_delta":
		delta := gjson.GetBytes(payload, "delta")
		if len(delta.Get("text").String()) > 0 || len(delta.Get("thinking").String()) > 0 || len(delta.Get("partial_json").String()) > 0 || len(delta.Get("signature").String()) > 0 {
			return true
		}
		return false
	case "content_block_start":
		cb := gjson.GetBytes(payload, "content_block")
		if len(cb.Get("text").String()) > 0 || len(cb.Get("thinking").String()) > 0 {
			return true
		}
		return cb.Get("type").String() == "tool_use" && len(cb.Get("name").String()) > 0
	case "message_delta":
		if substantive {
			return false
		}
		return len(gjson.GetBytes(payload, "delta.stop_reason").String()) > 0
	case "message_stop":
		return !substantive
	case "error":
		return true
	case "message_start", "ping", "content_block_stop":
		return false
	default:
		if gjson.GetBytes(payload, "content").IsArray() {
			for _, block := range gjson.GetBytes(payload, "content").Array() {
				if len(block.Get("text").String()) > 0 || len(block.Get("thinking").String()) > 0 {
					return true
				}
				if block.Get("type").String() == "tool_use" && len(block.Get("name").String()) > 0 {
					return true
				}
			}
		}
		return false
	}
}

// IsChatTokenEvent reports whether an OpenAI Chat Completions chunk carries
// substantive output token content.
func IsChatTokenEvent(payload []byte) bool {
	return chatTokenEvent(payload, false)
}

func chatTokenEvent(payload []byte, substantive bool) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}
	if bytes.Equal(payload, []byte("data: [DONE]")) || bytes.Equal(payload, []byte("[DONE]")) {
		return !substantive
	}
	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
		if len(payload) == 0 {
			return false
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			return !substantive
		}
	}
	if gjson.GetBytes(payload, "error.message").Exists() || gjson.GetBytes(payload, "error").Exists() {
		return true
	}
	choices := gjson.GetBytes(payload, "choices").Array()
	if len(choices) == 0 {
		return false
	}
	for _, choice := range choices {
		delta := choice.Get("delta")
		if delta.Exists() {
			if len(delta.Get("content").String()) > 0 || len(delta.Get("reasoning_content").String()) > 0 || len(delta.Get("reasoning").String()) > 0 || len(delta.Get("refusal").String()) > 0 {
				return true
			}
			for _, tc := range delta.Get("tool_calls").Array() {
				if len(tc.Get("function.arguments").String()) > 0 || len(tc.Get("function.name").String()) > 0 || len(tc.Get("custom.input").String()) > 0 {
					return true
				}
			}
		}
		message := choice.Get("message")
		if message.Exists() {
			if len(message.Get("content").String()) > 0 || len(message.Get("reasoning_content").String()) > 0 || len(message.Get("refusal").String()) > 0 {
				return true
			}
			for _, tc := range message.Get("tool_calls").Array() {
				if len(tc.Get("function.arguments").String()) > 0 || len(tc.Get("function.name").String()) > 0 {
					return true
				}
			}
		}
		if !substantive && len(choice.Get("finish_reason").String()) > 0 {
			return true
		}
	}
	return false
}

// IsGeminiTokenEvent reports whether a Gemini or Antigravity chunk carries
// substantive output token content.
func IsGeminiTokenEvent(payload []byte) bool {
	return geminiTokenEvent(payload, false)
}

func geminiTokenEvent(payload []byte, substantive bool) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}
	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
		if len(payload) == 0 {
			return false
		}
	}
	if gjson.GetBytes(payload, "error.message").Exists() || gjson.GetBytes(payload, "error").Exists() || gjson.GetBytes(payload, "response.error").Exists() {
		return true
	}
	candidatesNode := gjson.GetBytes(payload, "candidates")
	if !candidatesNode.Exists() {
		candidatesNode = gjson.GetBytes(payload, "response.candidates")
	}
	candidates := candidatesNode.Array()
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		for _, part := range candidate.Get("content.parts").Array() {
			if len(part.Get("text").String()) > 0 || len(part.Get("thoughtText").String()) > 0 || len(part.Get("functionCall.name").String()) > 0 || len(part.Get("inlineData.data").String()) > 0 {
				return true
			}
			if thought := part.Get("thought"); thought.Type == gjson.String && len(thought.String()) > 0 {
				return true
			}
		}
		if !substantive && len(candidate.Get("finishReason").String()) > 0 {
			return true
		}
	}
	return false
}

// IsResponsesTokenEvent reports whether an OpenAI or Codex Responses event
// carries substantive output.
func IsResponsesTokenEvent(payload []byte) bool {
	return responsesTokenEvent(payload, false)
}

func responsesTokenEvent(payload []byte, substantive bool) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}
	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
		if len(payload) == 0 {
			return false
		}
	}
	eventType := gjson.GetBytes(payload, "type").String()
	switch eventType {
	case "response.reasoning_summary_text.delta",
		"response.reasoning.delta",
		"response.reasoning_text.delta",
		"response.output_text.delta",
		"response.text.delta",
		"response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta",
		"response.code_interpreter_call_code.delta",
		"response.mcp_call_arguments.delta",
		"response.shell_call_command.delta",
		"response.refusal.delta",
		"response.audio.transcript.delta":
		return len(gjson.GetBytes(payload, "delta").String()) > 0
	case "response.audio.delta":
		return len(gjson.GetBytes(payload, "delta").String()) > 0 || len(gjson.GetBytes(payload, "data").String()) > 0
	case "response.image_generation_call.partial_image":
		return len(gjson.GetBytes(payload, "partial_image_b64").String()) > 0
	case "response.shell_call_command.added":
		return len(gjson.GetBytes(payload, "command").String()) > 0
	case "response.reasoning_summary_text.done",
		"response.reasoning_text.done",
		"response.output_text.done":
		return len(gjson.GetBytes(payload, "text").String()) > 0
	case "response.refusal.done":
		return len(gjson.GetBytes(payload, "refusal").String()) > 0
	case "response.function_call_arguments.done",
		"response.mcp_call_arguments.done":
		return len(gjson.GetBytes(payload, "arguments").String()) > 0
	case "response.custom_tool_call_input.done":
		return len(gjson.GetBytes(payload, "input").String()) > 0
	case "response.code_interpreter_call_code.done":
		return len(gjson.GetBytes(payload, "code").String()) > 0
	case "response.shell_call_command.done":
		return len(gjson.GetBytes(payload, "command").String()) > 0
	case "response.reasoning_summary_part.done":
		return len(gjson.GetBytes(payload, "part.text").String()) > 0
	case "response.content_part.done":
		return len(gjson.GetBytes(payload, "part.text").String()) > 0 || len(gjson.GetBytes(payload, "part.refusal").String()) > 0
	case "response.output_item.done":
		switch gjson.GetBytes(payload, "item.type").String() {
		case "function_call":
			return len(gjson.GetBytes(payload, "item.arguments").String()) > 0
		case "custom_tool_call":
			return len(gjson.GetBytes(payload, "item.input").String()) > 0
		case "message":
			for _, content := range gjson.GetBytes(payload, "item.content").Array() {
				if len(content.Get("text").String()) > 0 || len(content.Get("refusal").String()) > 0 {
					return true
				}
			}
		}
		return false
	case "response.completed", "response.done", "response.incomplete", "response.failed":
		return !substantive
	case "error":
		return true
	default:
		return false
	}
}

// IsInteractionsTokenEvent reports whether an Interactions SSE frame carries
// substantive output. Handshake events such as interaction.created, step.start,
// and step.stop do not count.
func IsInteractionsTokenEvent(payload []byte) bool {
	payload = interactionsEventJSON(payload)
	if len(payload) == 0 {
		return false
	}
	if gjson.GetBytes(payload, "error.message").Exists() || gjson.GetBytes(payload, "error").Exists() {
		return true
	}
	switch gjson.GetBytes(payload, "event_type").String() {
	case "step.delta":
		return interactionsDeltaHasToken(gjson.GetBytes(payload, "delta"))
	case "error":
		return true
	default:
		return false
	}
}

func interactionsEventJSON(payload []byte) []byte {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil
	}
	if bytes.Contains(payload, []byte("\n")) {
		for _, line := range bytes.Split(payload, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if bytes.HasPrefix(line, []byte("data:")) {
				payload = line
				break
			}
		}
	}
	if bytes.HasPrefix(payload, []byte("event:")) {
		if idx := bytes.IndexByte(payload, '\n'); idx >= 0 {
			payload = bytes.TrimSpace(payload[idx+1:])
		}
	}
	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
	}
	return payload
}

func interactionsDeltaHasToken(delta gjson.Result) bool {
	if !delta.Exists() {
		return false
	}
	switch delta.Get("type").String() {
	case "text":
		return len(delta.Get("text").String()) > 0 || len(delta.Get("content.text").String()) > 0
	case "thought_summary":
		return len(delta.Get("content.text").String()) > 0 || len(delta.Get("text").String()) > 0
	case "arguments_delta":
		return len(strings.TrimSpace(delta.Get("arguments").String())) > 0
	case "function_result":
		result := delta.Get("result")
		if !result.Exists() {
			return false
		}
		raw := strings.TrimSpace(result.Raw)
		return raw != "" && raw != "{}" && raw != "null"
	default:
		return len(delta.Get("text").String()) > 0 || len(delta.Get("content.text").String()) > 0 || len(delta.Get("image").String()) > 0
	}
}
