package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchStreamFirstTokenTimeout(t *testing.T) {
	initial := 12
	cfg := &config.Config{GeminiKey: []config.GeminiKey{{
		APIKey:                         "key",
		StreamFirstTokenTimeoutSeconds: &initial,
	}}}
	handler := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

	patch := func(fields string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/gemini-api-key", strings.NewReader(`{"index":0,"value":{`+fields+`}}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		handler.PatchGeminiKey(ctx)
		return rec
	}

	if rec := patch(`"prefix":"p"`); rec.Code != http.StatusOK {
		t.Fatalf("unrelated patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds == nil || *cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds != 12 {
		t.Fatalf("timeout = %v, want 12 preserved", cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds)
	}

	if rec := patch(`"stream-first-token-timeout-seconds":null`); rec.Code != http.StatusOK {
		t.Fatalf("null patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds != nil {
		t.Fatalf("timeout = %v, want cleared", *cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds)
	}

	if rec := patch(`"stream-first-token-timeout-seconds":"soon"`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if rec := patch(`"stream-first-token-timeout-seconds":3601`); rec.Code != http.StatusBadRequest {
		t.Fatalf("over-max status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
