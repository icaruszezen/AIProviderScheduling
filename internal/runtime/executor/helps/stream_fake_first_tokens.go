package helps

import (
	"bytes"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

// MetadataStreamFakeFirstTokens is the Auth.Metadata key for Codex probe tokens.
const MetadataStreamFakeFirstTokens = "stream_fake_first_tokens"

// StreamFakeFirstTokensFromAuth returns sanitized Codex fake first-token probes.
func StreamFakeFirstTokensFromAuth(auth *cliproxyauth.Auth) []string {
	if auth == nil || len(auth.Metadata) == 0 {
		return nil
	}
	raw, ok := auth.Metadata[MetadataStreamFakeFirstTokens]
	if !ok || raw == nil {
		return nil
	}
	switch tokens := raw.(type) {
	case []string:
		return config.SanitizeStreamFakeFirstTokens(tokens)
	case []any:
		out := make([]string, 0, len(tokens))
		for _, item := range tokens {
			if text, isString := item.(string); isString {
				out = append(out, text)
			}
		}
		return config.SanitizeStreamFakeFirstTokens(out)
	default:
		return nil
	}
}

// ResponsesVisibleTextToken extracts the exact visible text carried by a
// Responses streaming text event. The returned string is not trimmed.
func ResponsesVisibleTextToken(payload []byte) (string, bool) {
	payload = stripResponsesSSEPayload(payload)
	if len(payload) == 0 {
		return "", false
	}
	eventType := gjson.GetBytes(payload, "type").String()
	var node gjson.Result
	switch eventType {
	case "response.output_text.delta",
		"response.reasoning_text.delta",
		"response.reasoning_summary_text.delta",
		"response.text.delta",
		"response.reasoning.delta":
		node = gjson.GetBytes(payload, "delta")
	case "response.output_text.done",
		"response.reasoning_text.done",
		"response.reasoning_summary_text.done":
		node = gjson.GetBytes(payload, "text")
	default:
		return "", false
	}
	if !node.Exists() || node.Type != gjson.String {
		return "", false
	}
	return node.String(), true
}

// IsStreamFakeFirstToken reports whether payload is a visible text event whose
// entire text equals one of the configured probe tokens.
func IsStreamFakeFirstToken(payload []byte, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	text, ok := ResponsesVisibleTextToken(payload)
	if !ok {
		return false
	}
	for _, token := range tokens {
		if text == token {
			return true
		}
	}
	return false
}

// IsResponsesHoldableLifecycle reports whether a Responses event has no generated
// token yet and is therefore safe to hold while probing for a fake first token.
func IsResponsesHoldableLifecycle(payload []byte) bool {
	payload = stripResponsesSSEPayload(payload)
	if len(payload) == 0 {
		return true
	}
	switch gjson.GetBytes(payload, "type").String() {
	case "error", "response.failed", "response.completed", "response.done", "response.incomplete":
		return false
	}
	return !IsResponsesTokenEvent(payload)
}

func stripResponsesSSEPayload(payload []byte) []byte {
	payload = bytes.TrimSpace(payload)
	if !bytes.HasPrefix(payload, []byte("data:")) {
		return payload
	}
	prefixLen := len("data:")
	if bytes.HasPrefix(payload, []byte("data: ")) {
		prefixLen = len("data: ")
	}
	return bytes.TrimSpace(payload[prefixLen:])
}
