package config

import "testing"

func TestChannelMonitorNormalizedRefresh(t *testing.T) {
	cfg := ChannelMonitorConfig{RefreshIntervalSeconds: 15, AuthIndexes: []string{" a ", "a", ""}}.Normalized()
	if cfg.RefreshIntervalSeconds != 300 {
		t.Fatalf("refresh = %d, want 300", cfg.RefreshIntervalSeconds)
	}
	if len(cfg.AuthIndexes) != 1 || cfg.AuthIndexes[0] != "a" {
		t.Fatalf("auth indexes = %#v", cfg.AuthIndexes)
	}
	if cfg.IgnoredErrorCategories != nil {
		t.Fatal("nil ignored list should stay nil")
	}
}
