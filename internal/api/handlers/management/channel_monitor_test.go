package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/channelmonitor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetChannelMonitorSummariesShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	cfg := config.ChannelMonitorConfig{
		Enabled:                true,
		DatabasePath:           filepath.Join(dir, "channel-monitor.db"),
		RefreshIntervalSeconds: 60,
	}
	monitor := channelmonitor.New(filepath.Join(dir, "config.yaml"), cfg)
	now := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC)
	monitor.ApplyConfig(cfg)
	t.Cleanup(monitor.Stop)
	// Reach the unexported adder through the public ingest path by flushing a direct record
	// via the service query after using HandleUsage only when started. Add through a tiny
	// exported helper instead: run one record by opening summaries after Flush of records
	// published with the test hook below.
	monitor.ObserveForTest(usage.Record{
		Provider:    "gemini",
		AuthIndex:   "idx-1",
		Model:       "gemini-2.5-pro",
		RequestedAt: now,
	})

	handler := NewHandler(&config.Config{ChannelMonitor: cfg}, filepath.Join(dir, "config.yaml"), nil)
	handler.SetChannelMonitor(monitor)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/channel-monitor/summaries?range=90m&auth_index=idx-1", nil)
	handler.GetChannelMonitorSummaries(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Enabled bool `json:"enabled"`
		Items   []struct {
			AuthIndex      string `json:"auth_index"`
			Present        bool   `json:"present"`
			SuccessRequest int64  `json:"success_requests"`
			Metrics        struct {
				SuccessRequests int64 `json:"success_requests"`
			} `json:"metrics"`
			Buckets []struct {
				BucketStart time.Time `json:"bucket_start"`
			} `json:"buckets"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, recorder.Body.String())
	}
	if !payload.Enabled || len(payload.Items) != 1 || payload.Items[0].AuthIndex != "idx-1" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].Metrics.SuccessRequests != 1 {
		t.Fatalf("success = %d", payload.Items[0].Metrics.SuccessRequests)
	}
	if payload.Items[0].Present {
		t.Fatal("missing auth manager should mark the credential absent")
	}
	if len(payload.Items[0].Buckets) == 0 {
		t.Fatal("expected thumbnail buckets")
	}
}

func TestPutChannelMonitorRejectsInvalidRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{ChannelMonitor: config.ChannelMonitorConfig{Enabled: true, RefreshIntervalSeconds: 300}}
	handler := NewHandler(cfg, filepath.Join(t.TempDir(), "config.yaml"), nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/channel-monitor/config", strings.NewReader(`{"enabled":true,"refresh-interval-seconds":15}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.PutChannelMonitorConfig(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if cfg.ChannelMonitor.RefreshIntervalSeconds != 300 || !cfg.ChannelMonitor.Enabled {
		t.Fatalf("config changed on rejected write: %+v", cfg.ChannelMonitor)
	}
}

func TestPutChannelMonitorRejectsUnknownCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{ChannelMonitor: config.ChannelMonitorConfig{Enabled: true}}
	handler := NewHandler(cfg, filepath.Join(t.TempDir(), "config.yaml"), nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/channel-monitor/config", strings.NewReader(`{"enabled":true,"ignored-error-categories":["not-a-real"]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.PutChannelMonitorConfig(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if cfg.ChannelMonitor.IgnoredErrorCategories != nil {
		t.Fatalf("ignored categories = %#v", cfg.ChannelMonitor.IgnoredErrorCategories)
	}
}

func TestPatchChannelMonitorKeepsOmittedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\nchannel-monitor:\n  enabled: true\n  refresh-interval-seconds: 300\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{ChannelMonitor: config.ChannelMonitorConfig{Enabled: true, RefreshIntervalSeconds: 300, DatabasePath: filepath.Join(dir, "channel-monitor.db")}}
	handler := NewHandler(cfg, configPath, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/channel-monitor/config", strings.NewReader(`{"refresh-interval-seconds":60}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.PutChannelMonitorConfig(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	handler.mu.Lock()
	patched := cfg.ChannelMonitor
	handler.mu.Unlock()
	if !patched.Enabled || patched.RefreshIntervalSeconds != 60 {
		t.Fatalf("patched config = %+v", patched)
	}
	if patched.DatabasePath == "" {
		t.Fatal("patch cleared database-path")
	}
}

func TestPutChannelMonitorRollsBackWhenSaveFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{ChannelMonitor: config.ChannelMonitorConfig{Enabled: true, RefreshIntervalSeconds: 300}}
	handler := NewHandler(cfg, filepath.Join(t.TempDir(), "missing", "config.yaml"), nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/channel-monitor/config", strings.NewReader(`{"enabled":false,"refresh-interval-seconds":60}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.PutChannelMonitorConfig(ctx)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !cfg.ChannelMonitor.Enabled || cfg.ChannelMonitor.RefreshIntervalSeconds != 300 {
		t.Fatalf("config left dirty after failed save: %+v", cfg.ChannelMonitor)
	}
}
