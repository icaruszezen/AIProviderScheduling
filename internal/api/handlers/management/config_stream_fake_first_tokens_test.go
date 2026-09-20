package management

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPatchCodexStreamFakeFirstTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		CodexKey: []config.CodexKey{{
			APIKey:                "key",
			BaseURL:               "https://codex.example.com",
			StreamFakeFirstTokens: []string{" "}}}}
	h := &Handler{cfg: cfg, configFilePath: writeTestConfigFile(t)}

	patch := func(fields string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		body := `{"index":0,"value":{` + fields + `}}`
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/codex-api-key", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		h.PatchCodexKey(ctx)
		return rec
	}

	if rec := patch(`"prefix":"p"`); rec.Code != http.StatusOK {
		t.Fatalf("unrelated patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(cfg.CodexKey[0].StreamFakeFirstTokens, []string{" "}) {
		t.Fatalf("tokens after unrelated patch = %#v, want [\" \"]", cfg.CodexKey[0].StreamFakeFirstTokens)
	}

	if rec := patch(`"stream-fake-first-tokens":[" ","-"]`); rec.Code != http.StatusOK {
		t.Fatalf("set patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(cfg.CodexKey[0].StreamFakeFirstTokens, []string{" ", "-"}) {
		t.Fatalf("tokens after set = %#v, want [\" \", \"-\"]", cfg.CodexKey[0].StreamFakeFirstTokens)
	}

	if rec := patch(`"stream-fake-first-tokens":[]`); rec.Code != http.StatusOK {
		t.Fatalf("clear patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if cfg.CodexKey[0].StreamFakeFirstTokens != nil {
		t.Fatalf("tokens after clear = %#v, want nil", cfg.CodexKey[0].StreamFakeFirstTokens)
	}

	cfg.CodexKey[0].StreamFakeFirstTokens = []string{"-"}
	if rec := patch(`"stream-fake-first-tokens":null`); rec.Code != http.StatusOK {
		t.Fatalf("null patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(cfg.CodexKey[0].StreamFakeFirstTokens, []string{"-"}) {
		t.Fatalf("tokens after null = %#v, want [\"-\"]", cfg.CodexKey[0].StreamFakeFirstTokens)
	}
}
