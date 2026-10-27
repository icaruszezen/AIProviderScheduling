package claude

import (
	. "github.com/icaruszezen/AIProviderScheduling/v7/internal/constant"
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/interfaces"
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		Claude,
		OpenAI,
		ConvertClaudeRequestToOpenAI,
		interfaces.TranslateResponse{
			Stream:     ConvertOpenAIResponseToClaude,
			NonStream:  ConvertOpenAIResponseToClaudeNonStream,
			TokenCount: ClaudeTokenCount,
		},
	)
}
