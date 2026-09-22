package channelmonitor

import "github.com/router-for-me/CLIProxyAPI/v7/internal/config"

const (
	HealthUnknown  = "unknown"
	HealthHealthy  = "healthy"
	HealthWarning  = "warning"
	HealthCritical = "critical"
)

// DefaultHealthThresholds matches the sub2api V2 factory defaults.
// Cache rates of 0/0 mean cache misses do not affect the score.
func DefaultHealthThresholds() config.ChannelMonitorHealthThresholds {
	return config.ChannelMonitorHealthThresholds{
		MinimumSample:     50,
		WarningErrorRate:  0.05,
		CriticalErrorRate: 0.20,
		TargetTTFTMs:      3000,
		WarningTTFTMs:     3000,
		CriticalTTFTMs:    10000,
		WarningCacheRate:  0,
		CriticalCacheRate: 0,
		ErrorWeight:       0.60,
		TTFTWeight:        0.20,
		CacheWeight:       0.20,
	}
}

func normalizeThresholds(in config.ChannelMonitorHealthThresholds) config.ChannelMonitorHealthThresholds {
	def := DefaultHealthThresholds()
	if in.MinimumSample <= 0 {
		in.MinimumSample = def.MinimumSample
	}
	if in.MinimumSample > 10000 {
		in.MinimumSample = 10000
	}
	if in.WarningErrorRate <= 0 {
		in.WarningErrorRate = def.WarningErrorRate
	}
	if in.CriticalErrorRate <= 0 {
		in.CriticalErrorRate = def.CriticalErrorRate
	}
	if in.CriticalErrorRate < in.WarningErrorRate {
		in.CriticalErrorRate = in.WarningErrorRate
	}
	if in.TargetTTFTMs <= 0 {
		in.TargetTTFTMs = def.TargetTTFTMs
	}
	if in.WarningTTFTMs <= 0 {
		in.WarningTTFTMs = def.WarningTTFTMs
	}
	if in.WarningTTFTMs < in.TargetTTFTMs {
		in.WarningTTFTMs = in.TargetTTFTMs + 1
	}
	if in.CriticalTTFTMs <= 0 {
		in.CriticalTTFTMs = def.CriticalTTFTMs
	}
	if in.CriticalTTFTMs < in.WarningTTFTMs {
		in.CriticalTTFTMs = in.WarningTTFTMs
	}
	if in.WarningCacheRate < 0 {
		in.WarningCacheRate = 0
	}
	if in.CriticalCacheRate < 0 {
		in.CriticalCacheRate = 0
	}
	if in.WarningCacheRate > 1 {
		in.WarningCacheRate = 1
	}
	if in.CriticalCacheRate > 1 {
		in.CriticalCacheRate = 1
	}
	if in.CriticalCacheRate > in.WarningCacheRate {
		in.CriticalCacheRate = in.WarningCacheRate
	}
	if in.ErrorWeight <= 0 && in.TTFTWeight <= 0 && in.CacheWeight <= 0 {
		in.ErrorWeight = def.ErrorWeight
		in.TTFTWeight = def.TTFTWeight
		in.CacheWeight = def.CacheWeight
	}
	return in
}

// Health is the discrete band plus the continuous 0–100 score.
type Health struct {
	Overall        string                                `json:"overall"`
	ErrorRate      string                                `json:"error_rate"`
	TTFT           string                                `json:"ttft"`
	Cache          string                                `json:"cache"`
	Score          *float64                              `json:"score,omitempty"`
	ErrorRateScore *float64                              `json:"error_rate_score,omitempty"`
	TTFTScore      *float64                              `json:"ttft_score,omitempty"`
	CacheScore     *float64                              `json:"cache_score,omitempty"`
	MinimumSample  int64                                 `json:"minimum_sample"`
	Thresholds     config.ChannelMonitorHealthThresholds `json:"thresholds"`
}

func healthFor(metrics Metric, thresholds config.ChannelMonitorHealthThresholds) Health {
	thresholds = normalizeThresholds(thresholds)
	result := Health{
		Overall:       HealthUnknown,
		ErrorRate:     HealthUnknown,
		TTFT:          HealthUnknown,
		Cache:         HealthUnknown,
		MinimumSample: thresholds.MinimumSample,
		Thresholds:    thresholds,
	}

	type scored struct {
		score  float64
		weight float64
	}
	parts := make([]scored, 0, 3)

	if metrics.RequestCount >= result.MinimumSample {
		score := errorRateScore(metrics.ErrorRate, thresholds.CriticalErrorRate)
		result.ErrorRateScore = &score
		result.ErrorRate = higherIsWorse(metrics.ErrorRate, thresholds.WarningErrorRate, thresholds.CriticalErrorRate)
		parts = append(parts, scored{score: score, weight: thresholds.ErrorWeight})
	}
	ttftMs := metrics.TTFT.P50Ms
	if ttftMs == nil {
		ttftMs = metrics.TTFT.P95Ms
	}
	if metrics.TTFT.SampleCount >= result.MinimumSample && ttftMs != nil {
		score := ttftScore(float64(*ttftMs), float64(thresholds.TargetTTFTMs), float64(thresholds.CriticalTTFTMs))
		result.TTFTScore = &score
		result.TTFT = higherIsWorse(float64(*ttftMs), float64(thresholds.WarningTTFTMs), float64(thresholds.CriticalTTFTMs))
		parts = append(parts, scored{score: score, weight: thresholds.TTFTWeight})
	}
	if metrics.CacheRateDenominator >= result.MinimumSample {
		score := cacheRateScore(metrics.CacheRate)
		if thresholds.WarningCacheRate <= 0 && thresholds.CriticalCacheRate <= 0 {
			score = 100
		}
		result.CacheScore = &score
		result.Cache = cacheBand(metrics.CacheRate, thresholds.WarningCacheRate, thresholds.CriticalCacheRate)
		parts = append(parts, scored{score: score, weight: thresholds.CacheWeight})
	}
	if len(parts) == 0 {
		return result
	}
	var weightSum, scoreSum float64
	for _, part := range parts {
		weightSum += part.weight
		scoreSum += part.weight * part.score
	}
	if weightSum <= 0 {
		return result
	}
	overall := scoreSum / weightSum
	result.Score = &overall
	result.Overall = scoreBand(overall)
	return result
}

func errorRateScore(errorRate, critical float64) float64 {
	if critical <= 0 {
		critical = 0.05
	}
	if errorRate <= 0 {
		return 100
	}
	if errorRate >= critical {
		return 0
	}
	return 100 * (1 - errorRate/critical)
}

func ttftScore(p50Ms, targetMs, criticalMs float64) float64 {
	if targetMs <= 0 {
		targetMs = 3000
	}
	if criticalMs <= targetMs {
		criticalMs = targetMs * 2.4
	}
	if p50Ms <= targetMs {
		return 100
	}
	if p50Ms >= criticalMs {
		return 0
	}
	return 100 * (1 - (p50Ms-targetMs)/(criticalMs-targetMs))
}

func cacheRateScore(cacheRate float64) float64 {
	if cacheRate <= 0 {
		return 0
	}
	if cacheRate >= 1 {
		return 100
	}
	return 100 * cacheRate
}

func cacheBand(cacheRate, warning, critical float64) string {
	if cacheRate < critical {
		return HealthCritical
	}
	if cacheRate < warning {
		return HealthWarning
	}
	return HealthHealthy
}

func scoreBand(score float64) string {
	switch {
	case score >= 80:
		return HealthHealthy
	case score >= 50:
		return HealthWarning
	default:
		return HealthCritical
	}
}

func higherIsWorse(value, warning, critical float64) string {
	if value >= critical {
		return HealthCritical
	}
	if value >= warning {
		return HealthWarning
	}
	return HealthHealthy
}
