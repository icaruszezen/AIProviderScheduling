package gemini

import (
	. "github.com/icaruszezen/AIProviderScheduling/v7/internal/constant"
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/interfaces"
	"github.com/icaruszezen/AIProviderScheduling/v7/internal/translator/translator"
)

func init() {
	translator.Register(
		Gemini,
		Codex,
		ConvertGeminiRequestToCodex,
		interfaces.TranslateResponse{
			Stream:     ConvertCodexResponseToGemini,
			NonStream:  ConvertCodexResponseToGeminiNonStream,
			TokenCount: GeminiTokenCount,
		},
	)
}
