package helps

import (
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/thinking"
	apsauth "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/auth"
	apsexecutor "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/executor"
)

// APIKeyModelIsCompat reports whether the selected API-key model enables
// compatibility handling for Claude thinking blocks.
func APIKeyModelIsCompat(req apsexecutor.Request) bool {
	modelInfo, ok := apsauth.ResolvedAPIKeyModelInfo(req)
	return ok && modelInfo != nil && modelInfo.IsCompat
}

// ApplyRequestThinking preserves the registry lookup path unless the auth
// manager bound an exact configured API-key model definition to this attempt.
func ApplyRequestThinking(body []byte, req apsexecutor.Request, opts apsexecutor.Options, fromFormat, toFormat, provider string) ([]byte, error) {
	originalSource := opts.OriginalRequest
	if len(originalSource) == 0 {
		originalSource = req.Payload
	}
	summaryConfig := translatedRequestSummaryConfig(body, req.Payload, originalSource, req.Model, fromFormat, toFormat)
	if modelInfo, ok := apsauth.ResolvedAPIKeyModelInfo(req); ok {
		return thinking.ApplyThinkingWithModelInfoAndSummary(body, originalSource, req.Model, fromFormat, toFormat, provider, modelInfo, summaryConfig)
	}
	return thinking.ApplyThinkingWithSummary(body, req.Model, fromFormat, toFormat, provider, summaryConfig)
}
