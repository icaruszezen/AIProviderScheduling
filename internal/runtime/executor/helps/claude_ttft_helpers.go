package helps

import (
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// IsClaudeTokenEvent reports whether an Anthropic Messages SSE chunk carries
// substantive output token content (such as text delta, thinking delta, or tool input delta).
// It filters out message_start, ping, content_block_stop, and empty containers.
func IsClaudeTokenEvent(payload []byte) bool {
	return cliproxyexecutor.IsClaudeTokenEvent(payload)
}

// ObserveClaudeTokenEvent inspects an Anthropic Messages SSE chunk and records TTFT if the frame
// represents the first meaningful token event. It records first-packet arrival time as fallback
// and returns immediately with zero allocations once effective token TTFT is set.
func ObserveClaudeTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsClaudeTokenEvent(payload))
}
