package helps

import (
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// IsResponsesTokenEvent reports whether the given OpenAI/xAI Responses API WebSocket or SSE event
// payload carries an actual output token, reasoning trace, tool call argument, or multimodal delta,
// according to the official Responses API streaming / websocket specification plus Codex-compatible private events.
func IsResponsesTokenEvent(payload []byte) bool {
	return cliproxyexecutor.IsResponsesTokenEvent(payload)
}

// ObserveResponsesTokenEvent inspects a Responses API frame payload and records TTFT if the frame
// represents the first meaningful token event. It records the first packet arrival time as a fallback
// and exits immediately with zero allocations once effective token TTFT is set.
func ObserveResponsesTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsResponsesTokenEvent(payload))
}
