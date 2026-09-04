package claude

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func TestClaudeErrorExtractsOpenAIStyleUpstreamJSON(t *testing.T) {
	handler := &ClaudeCodeAPIHandler{}
	msg := &interfaces.ErrorMessage{
		StatusCode: http.StatusBadRequest,
		Error:      errors.New(`{"error":{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","type":"invalid_request_error","code":"context_too_large"}}`),
	}

	got := handler.toClaudeError(msg)

	if got.Type != "error" {
		t.Fatalf("type = %q, want error", got.Type)
	}
	if got.Error.Type != "invalid_request_error" {
		t.Fatalf("error.type = %q, want invalid_request_error", got.Error.Type)
	}
	if got.Error.Message != "Your input exceeds the context window of this model. Please adjust your input and try again." {
		t.Fatalf("error.message = %q", got.Error.Message)
	}
}

func TestClaudeErrorExtractsClaudeStyleUpstreamJSON(t *testing.T) {
	handler := &ClaudeCodeAPIHandler{}
	msg := &interfaces.ErrorMessage{
		StatusCode: http.StatusTooManyRequests,
		Error:      errors.New(`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."},"request_id":"req_123"}`),
	}

	got := handler.toClaudeError(msg)

	if got.Error.Type != "rate_limit_error" {
		t.Fatalf("error.type = %q, want rate_limit_error", got.Error.Type)
	}
	if got.Error.Message != "This request would exceed your account's rate limit. Please try again later." {
		t.Fatalf("error.message = %q", got.Error.Message)
	}
}

func TestWriteClaudeErrorResponseUsesClaudeEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	handler := &ClaudeCodeAPIHandler{}
	msg := &interfaces.ErrorMessage{
		StatusCode: http.StatusBadRequest,
		Error:      errors.New(`{"error":{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","type":"invalid_request_error","code":"context_too_large"}}`),
	}

	handler.WriteErrorResponse(c, msg)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	body := recorder.Body.Bytes()
	if got := gjson.GetBytes(body, "type").String(); got != "error" {
		t.Fatalf("type = %q, want error; body=%s", got, body)
	}
	if got := gjson.GetBytes(body, "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("error.type = %q, want invalid_request_error; body=%s", got, body)
	}
	if got := gjson.GetBytes(body, "error.message").String(); got != "Your input exceeds the context window of this model. Please adjust your input and try again." {
		t.Fatalf("error.message = %q; body=%s", got, body)
	}
}

func TestWriteClaudeErrorResponse_IncludesRetryAfterForModelCooldownDefaultSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	handler := &ClaudeCodeAPIHandler{}

	cooldownErr := coreauth.NewManager(nil, nil, nil)
	_ = cooldownErr
	// Create mock model cooldown error
	msg := &interfaces.ErrorMessage{
		StatusCode: http.StatusTooManyRequests,
		Error:      coreauth.NewModelCooldownError("claude-sonnet-4-6", "claude", 20*time.Second),
	}

	handler.WriteErrorResponse(c, msg)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if got := recorder.Header().Get("Retry-After"); got != "20" {
		t.Fatalf("Retry-After = %q, want 20", got)
	}
}

func TestWriteClaudeErrorResponse_HidesNoAvailableChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	handler := &ClaudeCodeAPIHandler{}

	upstream := `{"error":{"message":"No available channel for model gpt-5.6-sol under group promo"}}`
	msg := &interfaces.ErrorMessage{
		StatusCode:             http.StatusServiceUnavailable,
		Error:                  errors.New(upstream),
		DirectResponse:         true,
		Body:                   []byte(upstream),
		HideNoAvailableChannel: true,
	}

	handler.WriteErrorResponse(c, msg)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	body := recorder.Body.Bytes()
	if gjson.GetBytes(body, "type").String() != "error" {
		t.Fatalf("type = %q, want error; body=%s", gjson.GetBytes(body, "type").String(), body)
	}
	if msg := gjson.GetBytes(body, "error.message").String(); msg != "Service Unavailable" {
		t.Fatalf("error.message = %q, want Service Unavailable; body=%s", msg, body)
	}
	if strings.Contains(string(body), "No available channel") {
		t.Fatalf("downstream body leaked channel details: %s", body)
	}

	logged, ok := c.Get("API_RESPONSE")
	if !ok {
		t.Fatal("API_RESPONSE was not captured")
	}
	loggedBytes, ok := logged.([]byte)
	if !ok {
		t.Fatalf("API_RESPONSE type = %T", logged)
	}
	if !bytes.Contains(loggedBytes, []byte("No available channel for model")) {
		t.Fatalf("request log lost original error: %s", loggedBytes)
	}
}

func TestForwardClaudeStream_HidesNoAvailableChannelKeepsLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	handler := &ClaudeCodeAPIHandler{BaseAPIHandler: handlers.NewBaseAPIHandlers(nil, nil)}

	upstream := `{"error":{"message":"No available channel for model gpt-5.6-sol under group promo"}}`
	data := make(chan []byte)
	close(data)
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- &interfaces.ErrorMessage{
		StatusCode:             http.StatusServiceUnavailable,
		Error:                  errors.New(upstream),
		DirectResponse:         true,
		Body:                   []byte(upstream),
		HideNoAvailableChannel: true,
	}
	close(errs)

	handler.forwardClaudeStream(c, recorder, func(error) {}, data, errs)

	body := recorder.Body.String()
	if strings.Contains(body, "No available channel") {
		t.Fatalf("downstream stream leaked channel details: %s", body)
	}
	if !strings.Contains(body, "Service Unavailable") {
		t.Fatalf("downstream stream = %s, want Service Unavailable", body)
	}

	logged, ok := c.Get("API_RESPONSE")
	if !ok {
		t.Fatal("API_RESPONSE was not captured")
	}
	loggedBytes, ok := logged.([]byte)
	if !ok {
		t.Fatalf("API_RESPONSE type = %T", logged)
	}
	if !bytes.Contains(loggedBytes, []byte("No available channel for model")) {
		t.Fatalf("request log lost original error: %s", loggedBytes)
	}
}

func TestPendingClaudeStreamErrorUsesBufferedError(t *testing.T) {
	wantErr := &interfaces.ErrorMessage{
		StatusCode: http.StatusBadRequest,
		Error:      errors.New(`{"error":{"message":"Your input exceeds the context window of this model. Please adjust your input and try again.","type":"invalid_request_error","code":"context_too_large"}}`),
	}
	errs := make(chan *interfaces.ErrorMessage, 1)
	errs <- wantErr
	close(errs)

	gotErr, ok := handlers.PendingStreamError(errs)
	if !ok {
		t.Fatal("expected pending stream error")
	}
	if gotErr != wantErr {
		t.Fatalf("pending error = %p, want %p", gotErr, wantErr)
	}
}
