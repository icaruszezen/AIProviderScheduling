package config

import "strings"

// ChannelMonitorConfig controls passive aggregation of real upstream traffic.
// A zero RefreshIntervalSeconds means the runtime default (300). A nil
// IgnoredErrorCategories list means the factory ignored set; an empty list
// ignores nothing.
type ChannelMonitorConfig struct {
	Enabled                bool                           `yaml:"enabled" json:"enabled"`
	RefreshIntervalSeconds int                            `yaml:"refresh-interval-seconds,omitempty" json:"refresh-interval-seconds,omitempty"`
	DatabasePath           string                         `yaml:"database-path,omitempty" json:"database-path,omitempty"`
	AuthIndexes            []string                       `yaml:"auth-indexes,omitempty" json:"auth-indexes,omitempty"`
	Providers              []string                       `yaml:"providers,omitempty" json:"providers,omitempty"`
	Models                 []string                       `yaml:"models,omitempty" json:"models,omitempty"`
	IgnoredErrorCategories []string                       `yaml:"ignored-error-categories,omitempty" json:"ignored-error-categories,omitempty"`
	HealthThresholds       ChannelMonitorHealthThresholds `yaml:"health-thresholds,omitempty" json:"health-thresholds,omitempty"`
}

// ChannelMonitorHealthThresholds scores aggregated traffic. Zero values fall
// back to factory defaults at runtime, except cache rates: 0/0 disables cache
// penalties.
type ChannelMonitorHealthThresholds struct {
	MinimumSample     int64   `yaml:"minimum-sample,omitempty" json:"minimum-sample,omitempty"`
	WarningErrorRate  float64 `yaml:"warning-error-rate,omitempty" json:"warning-error-rate,omitempty"`
	CriticalErrorRate float64 `yaml:"critical-error-rate,omitempty" json:"critical-error-rate,omitempty"`
	TargetTTFTMs      int64   `yaml:"target-ttft-ms,omitempty" json:"target-ttft-ms,omitempty"`
	WarningTTFTMs     int64   `yaml:"warning-ttft-ms,omitempty" json:"warning-ttft-ms,omitempty"`
	CriticalTTFTMs    int64   `yaml:"critical-ttft-ms,omitempty" json:"critical-ttft-ms,omitempty"`
	WarningCacheRate  float64 `yaml:"warning-cache-rate,omitempty" json:"warning-cache-rate,omitempty"`
	CriticalCacheRate float64 `yaml:"critical-cache-rate,omitempty" json:"critical-cache-rate,omitempty"`
	ErrorWeight       float64 `yaml:"error-weight,omitempty" json:"error-weight,omitempty"`
	TTFTWeight        float64 `yaml:"ttft-weight,omitempty" json:"ttft-weight,omitempty"`
	CacheWeight       float64 `yaml:"cache-weight,omitempty" json:"cache-weight,omitempty"`
}

// Normalized trims lists and replaces an invalid refresh interval with 300.
// Zero refresh and a nil ignored-category list are preserved so callers can
// still apply runtime defaults without rewriting config.yaml.
func (c ChannelMonitorConfig) Normalized() ChannelMonitorConfig {
	switch c.RefreshIntervalSeconds {
	case 0, 60, 300:
	default:
		c.RefreshIntervalSeconds = 300
	}
	c.DatabasePath = strings.TrimSpace(c.DatabasePath)
	c.AuthIndexes = uniqueTrimmed(c.AuthIndexes)
	c.Providers = uniqueTrimmed(c.Providers)
	c.Models = uniqueTrimmed(c.Models)
	c.IgnoredErrorCategories = uniqueTrimmed(c.IgnoredErrorCategories)
	if c.HealthThresholds.MinimumSample < 0 {
		c.HealthThresholds.MinimumSample = 0
	}
	if c.HealthThresholds.WarningErrorRate < 0 {
		c.HealthThresholds.WarningErrorRate = 0
	}
	if c.HealthThresholds.CriticalErrorRate < 0 {
		c.HealthThresholds.CriticalErrorRate = 0
	}
	if c.HealthThresholds.TargetTTFTMs < 0 {
		c.HealthThresholds.TargetTTFTMs = 0
	}
	if c.HealthThresholds.WarningTTFTMs < 0 {
		c.HealthThresholds.WarningTTFTMs = 0
	}
	if c.HealthThresholds.CriticalTTFTMs < 0 {
		c.HealthThresholds.CriticalTTFTMs = 0
	}
	if c.HealthThresholds.WarningCacheRate < 0 {
		c.HealthThresholds.WarningCacheRate = 0
	}
	if c.HealthThresholds.CriticalCacheRate < 0 {
		c.HealthThresholds.CriticalCacheRate = 0
	}
	if c.HealthThresholds.ErrorWeight < 0 {
		c.HealthThresholds.ErrorWeight = 0
	}
	if c.HealthThresholds.TTFTWeight < 0 {
		c.HealthThresholds.TTFTWeight = 0
	}
	if c.HealthThresholds.CacheWeight < 0 {
		c.HealthThresholds.CacheWeight = 0
	}
	return c
}

func uniqueTrimmed(values []string) []string {
	if values == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
