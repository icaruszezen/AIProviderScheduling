package helps

import (
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// IsChatTokenEvent reports whether the given OpenAI-compatible Chat Completions SSE or JSON chunk
// carries substantive output token content (such as text delta, reasoning content, or tool call arguments).
// It filters out container metadata, role announcements, and empty delta frames.
func IsChatTokenEvent(payload []byte) bool {
	return cliproxyexecutor.IsChatTokenEvent(payload)
}

// ObserveChatTokenEvent inspects an OpenAI Chat Completions chunk and records TTFT if the frame
// represents the first meaningful token event. It records first-packet arrival time as fallback
// and returns immediately with zero allocations once effective token TTFT is set.
func ObserveChatTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsChatTokenEvent(payload))
}
