package management

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchProviderRetryOverrideForEveryFamily(t *testing.T) {
	initialCount := 5
	initialCodes := []int{500}
	tests := []struct {
		name  string
		setup func(*config.Config)
		patch func(*Handler, *gin.Context)
		get   func(*config.Config) (*int, *[]int)
	}{
		{
			name: "gemini",
			setup: func(cfg *config.Config) {
				cfg.GeminiKey = []config.GeminiKey{{
					APIKey:                   "key",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchGeminiKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.GeminiKey[0].ProviderRetryCount, cfg.GeminiKey[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "interactions",
			setup: func(cfg *config.Config) {
				cfg.InteractionsKey = []config.GeminiKey{{
					APIKey:                   "key",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchInteractionsKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.InteractionsKey[0].ProviderRetryCount, cfg.InteractionsKey[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "claude",
			setup: func(cfg *config.Config) {
				cfg.ClaudeKey = []config.ClaudeKey{{
					APIKey:                   "key",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchClaudeKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.ClaudeKey[0].ProviderRetryCount, cfg.ClaudeKey[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "openai compatibility",
			setup: func(cfg *config.Config) {
				cfg.OpenAICompatibility = []config.OpenAICompatibility{{
					Name:                     "compat",
					BaseURL:                  "https://compat.example.com",
					APIKeyEntries:            []config.OpenAICompatibilityAPIKey{{APIKey: "key"}},
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchOpenAICompat,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.OpenAICompatibility[0].ProviderRetryCount, cfg.OpenAICompatibility[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "vertex",
			setup: func(cfg *config.Config) {
				cfg.VertexCompatAPIKey = []config.VertexCompatKey{{
					APIKey:                   "key",
					BaseURL:                  "https://vertex.example.com",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchVertexCompatKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.VertexCompatAPIKey[0].ProviderRetryCount, cfg.VertexCompatAPIKey[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "codex",
			setup: func(cfg *config.Config) {
				cfg.CodexKey = []config.CodexKey{{
					APIKey:                   "key",
					BaseURL:                  "https://codex.example.com",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchCodexKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.CodexKey[0].ProviderRetryCount, cfg.CodexKey[0].ProviderRetryStatusCodes
			},
		},
		{
			name: "xai",
			setup: func(cfg *config.Config) {
				cfg.XAIKey = []config.XAIKey{{
					APIKey:                   "key",
					BaseURL:                  "https://api.x.ai/v1",
					ProviderRetryCount:       &initialCount,
					ProviderRetryStatusCodes: &initialCodes,
				}}
			},
			patch: (*Handler).PatchXAIKey,
			get: func(cfg *config.Config) (*int, *[]int) {
				return cfg.XAIKey[0].ProviderRetryCount, cfg.XAIKey[0].ProviderRetryStatusCodes
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			tc.setup(cfg)
			h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

			patch := func(fields string) *httptest.ResponseRecorder {
				t.Helper()
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				body := fmt.Sprintf(`{"index":0,"value":{%s}}`, fields)
				ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/key", strings.NewReader(body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				tc.patch(h, ctx)
				return rec
			}

			// An unrelated patch leaves both fields untouched.
			if rec := patch(`"prefix":"p"`); rec.Code != http.StatusOK {
				t.Fatalf("unrelated patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			count, codes := tc.get(cfg)
			if count == nil || *count != 5 {
				t.Fatalf("provider-retry-count = %v, want 5 preserved", count)
			}
			if codes == nil || !reflect.DeepEqual(*codes, []int{500}) {
				t.Fatalf("provider-retry-status-codes = %v, want [500] preserved", codes)
			}

			// An empty array disables status-code triggered retries.
			if rec := patch(`"provider-retry-status-codes":[]`); rec.Code != http.StatusOK {
				t.Fatalf("empty codes patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			_, codes = tc.get(cfg)
			if codes == nil || len(*codes) != 0 {
				t.Fatalf("provider-retry-status-codes = %v, want explicit empty list", codes)
			}

			// An explicit null restores the inherited defaults.
			if rec := patch(`"provider-retry-count":null,"provider-retry-status-codes":null`); rec.Code != http.StatusOK {
				t.Fatalf("null patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			count, codes = tc.get(cfg)
			if count != nil {
				t.Fatalf("provider-retry-count = %v, want cleared", *count)
			}
			if codes != nil {
				t.Fatalf("provider-retry-status-codes = %v, want cleared", *codes)
			}

			// A malformed value is rejected instead of silently ignored.
			if rec := patch(`"provider-retry-count":"three"`); rec.Code != http.StatusBadRequest {
				t.Fatalf("malformed count status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
