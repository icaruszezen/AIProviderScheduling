package channelmonitor

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestSummariesIncludeUnflushedTrafficAndIgnoreAuthErrors(t *testing.T) {
	service := newTestService(t, config.ChannelMonitorConfig{Enabled: true})
	now := time.Date(2026, 9, 22, 3, 0, 30, 0, time.UTC)
	service.now = func() time.Time { return now }
	for i := 0; i < 40; i++ {
		service.addRecord(usage.Record{
			Provider:    "gemini",
			AuthIndex:   "auth-1",
			Model:       "gemini-2.5-pro",
			RequestedAt: now,
			Detail:      usage.Detail{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 5},
			TTFT:        100 * time.Millisecond,
			Latency:     200 * time.Millisecond,
		})
	}
	for i := 0; i < 10; i++ {
		service.addRecord(usage.Record{
			Provider:    "gemini",
			AuthIndex:   "auth-1",
			Model:       "gemini-2.5-pro",
			RequestedAt: now,
			Failed:      true,
			Fail:        usage.Failure{StatusCode: 401, Body: "invalid api key"},
		})
	}
	service.addRecord(usage.Record{Provider: "claude", AuthIndex: "other", Model: "claude", RequestedAt: now, Generate: usage.GenerateFlag(false)})

	summary, err := service.Summaries(Query{})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if !summary.Enabled || summary.Range != "90m" || len(summary.Items) != 1 {
		t.Fatalf("summary = enabled %t range %s items %d", summary.Enabled, summary.Range, len(summary.Items))
	}
	item := summary.Items[0]
	if item.AuthIndex != "auth-1" || item.Metrics.SuccessRequests != 40 || item.Metrics.ErrorRequests != 10 {
		t.Fatalf("item counts = %+v", item.Metrics)
	}
	if item.Metrics.ErrorRate != 0 {
		t.Fatalf("error rate = %v, want ignored auth failures excluded", item.Metrics.ErrorRate)
	}
	if item.Health.Overall != HealthHealthy {
		t.Fatalf("health = %s, want healthy", item.Health.Overall)
	}
	if item.Metrics.TTFT.P50Ms == nil || *item.Metrics.TTFT.P50Ms == 0 {
		t.Fatalf("ttft p50 = %#v", item.Metrics.TTFT.P50Ms)
	}
	if len(item.Buckets) != 18 {
		t.Fatalf("buckets = %d, want 18", len(item.Buckets))
	}

	errors, err := service.Errors(Query{AuthIndexes: []string{"auth-1"}})
	if err != nil {
		t.Fatalf("Errors() error = %v", err)
	}
	if len(errors.Items) != 1 || errors.Items[0].Category != CategoryAuthentication || !errors.Items[0].Ignored {
		t.Fatalf("errors = %+v", errors.Items)
	}
}

func TestAllowlistSkipsOtherProviders(t *testing.T) {
	service := newTestService(t, config.ChannelMonitorConfig{Enabled: true, Providers: []string{"gemini"}})
	now := time.Date(2026, 9, 22, 3, 0, 30, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.addRecord(usage.Record{Provider: "claude", AuthIndex: "a", Model: "claude", RequestedAt: now})
	summary, err := service.Summaries(Query{})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(summary.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(summary.Items))
	}
}

func TestFlushPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "channel-monitor.db")
	cfg := config.ChannelMonitorConfig{Enabled: true, DatabasePath: dbPath, RefreshIntervalSeconds: 60}
	service := New(configPath, cfg)
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.ApplyConfig(cfg)
	service.addRecord(usage.Record{Provider: "gemini", AuthIndex: "auth-9", Model: "gemini-2.5-flash", RequestedAt: now})
	if err := service.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	service.Stop()

	reopened := New(configPath, cfg)
	reopened.now = func() time.Time { return now }
	reopened.ApplyConfig(cfg)
	summary, err := reopened.Summaries(Query{AuthIndexes: []string{"auth-9"}})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(summary.Items) != 1 || summary.Items[0].Metrics.SuccessRequests != 1 {
		t.Fatalf("reopened summary = %+v", summary.Items)
	}
	reopened.Stop()
}

func TestFlushDuringQueryDoesNotDoubleCount(t *testing.T) {
	service := newTestService(t, config.ChannelMonitorConfig{Enabled: true})
	now := time.Date(2026, 9, 22, 3, 0, 30, 0, time.UTC)
	service.now = func() time.Time { return now }
	const records = 25
	for i := 0; i < records; i++ {
		service.addRecord(usage.Record{Provider: "gemini", AuthIndex: "auth-1", Model: "gemini-2.5-pro", RequestedAt: now})
	}
	service.beforeCollectLoad = func() {
		if err := service.Flush(); err != nil {
			t.Errorf("Flush() error = %v", err)
		}
	}
	summary, err := service.Summaries(Query{})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(summary.Items) != 1 || summary.Items[0].Metrics.SuccessRequests != records {
		t.Fatalf("success = %+v, want %d", summary.Items, records)
	}
}

func TestConcurrentFlushAndSummaryKeepsExactCount(t *testing.T) {
	service := newTestService(t, config.ChannelMonitorConfig{Enabled: true})
	now := time.Date(2026, 9, 22, 3, 0, 30, 0, time.UTC)
	service.now = func() time.Time { return now }
	const records = 100
	for i := 0; i < records; i++ {
		service.addRecord(usage.Record{Provider: "gemini", AuthIndex: "auth-1", Model: "gemini-2.5-pro", RequestedAt: now})
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := service.Flush(); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			summary, err := service.Summaries(Query{})
			if err != nil {
				errCh <- err
				return
			}
			got := int64(0)
			if len(summary.Items) > 0 {
				got = summary.Items[0].Metrics.SuccessRequests
			}
			if got > records {
				errCh <- fmt.Errorf("success = %d, exceeds %d", got, records)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if err := service.Flush(); err != nil {
		t.Fatalf("final Flush() error = %v", err)
	}
	summary, err := service.Summaries(Query{})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	if len(summary.Items) != 1 || summary.Items[0].Metrics.SuccessRequests != records {
		t.Fatalf("final success = %+v, want %d", summary.Items, records)
	}
}

func TestCoverageUsesMinuteBuckets(t *testing.T) {
	service := newTestService(t, config.ChannelMonitorConfig{Enabled: true})
	now := time.Date(2026, 9, 22, 10, 3, 30, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.addRecord(usage.Record{Provider: "gemini", AuthIndex: "auth-1", Model: "gemini-2.5-pro", RequestedAt: now})

	assertCoverage := func(summary SummaryResponse) {
		t.Helper()
		wantStart := time.Date(2026, 9, 22, 10, 3, 0, 0, time.UTC)
		if !summary.Coverage.CoverageStart.Equal(wantStart) {
			t.Fatalf("coverage start = %s, want %s", summary.Coverage.CoverageStart, wantStart)
		}
		if summary.Coverage.DataThrough.IsZero() || summary.Coverage.DataThrough.After(now) {
			t.Fatalf("data_through = %s, now = %s", summary.Coverage.DataThrough, now)
		}
	}
	summary, err := service.Summaries(Query{Range: "24h"})
	if err != nil {
		t.Fatalf("Summaries() error = %v", err)
	}
	assertCoverage(summary)
	if err = service.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	summary, err = service.Summaries(Query{Range: "24h"})
	if err != nil {
		t.Fatalf("Summaries() after flush error = %v", err)
	}
	assertCoverage(summary)
}

func newTestService(t *testing.T, cfg config.ChannelMonitorConfig) *Service {
	t.Helper()
	dir := t.TempDir()
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(dir, "channel-monitor.db")
	}
	if cfg.RefreshIntervalSeconds == 0 {
		cfg.RefreshIntervalSeconds = 60
	}
	service := New(filepath.Join(dir, "config.yaml"), cfg)
	service.ApplyConfig(cfg)
	t.Cleanup(service.Stop)
	return service
}
