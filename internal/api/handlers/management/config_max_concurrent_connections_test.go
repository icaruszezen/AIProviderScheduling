package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchMaxConcurrentConnections(t *testing.T) {
	initial := 4
	cfg := &config.Config{GeminiKey: []config.GeminiKey{{
		APIKey:                   "key",
		MaxConcurrentConnections: &initial,
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
	if cfg.GeminiKey[0].MaxConcurrentConnections == nil || *cfg.GeminiKey[0].MaxConcurrentConnections != 4 {
		t.Fatalf("limit = %v, want 4 preserved", cfg.GeminiKey[0].MaxConcurrentConnections)
	}

	if rec := patch(`"max-concurrent-connections":null`); rec.Code != http.StatusOK {
		t.Fatalf("null patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if cfg.GeminiKey[0].MaxConcurrentConnections != nil {
		t.Fatalf("limit = %v, want cleared", *cfg.GeminiKey[0].MaxConcurrentConnections)
	}

	if rec := patch(`"max-concurrent-connections":0`); rec.Code != http.StatusOK {
		t.Fatalf("zero patch status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if cfg.GeminiKey[0].MaxConcurrentConnections == nil || *cfg.GeminiKey[0].MaxConcurrentConnections != 0 {
		t.Fatalf("limit = %v, want 0", cfg.GeminiKey[0].MaxConcurrentConnections)
	}

	if rec := patch(`"max-concurrent-connections":"many"`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if rec := patch(`"max-concurrent-connections":1000001`); rec.Code != http.StatusBadRequest {
		t.Fatalf("over-max status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if rec := patch(`"max-concurrent-connections":-1`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
