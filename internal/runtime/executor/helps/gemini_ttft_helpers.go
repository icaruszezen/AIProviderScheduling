package helps

import (
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// IsGeminiTokenEvent reports whether a Google Gemini / Antigravity streaming chunk carries
// substantive output token content (such as text parts, thought traces, or function calls).
// It filters out metadata-only chunks (e.g. standalone usageMetadata) and empty parts.
func IsGeminiTokenEvent(payload []byte) bool {
	return cliproxyexecutor.IsGeminiTokenEvent(payload)
}

// ObserveGeminiTokenEvent inspects a Google Gemini / Antigravity streaming chunk and records TTFT
// if the frame represents the first meaningful token event. It records first-packet arrival time
// as fallback and returns immediately with zero allocations once effective token TTFT is set.
func ObserveGeminiTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsGeminiTokenEvent(payload))
}
