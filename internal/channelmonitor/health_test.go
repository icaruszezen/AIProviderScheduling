package channelmonitor

import "testing"

func TestHealthScoreBands(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	unknown := healthFor(Metric{RequestCount: 10, ErrorRate: 0}, thresholds)
	if unknown.Overall != HealthUnknown || unknown.Score != nil {
		t.Fatalf("small sample = %#v, want unknown without score", unknown)
	}

	healthy := healthFor(Metric{RequestCount: 50, ErrorRate: 0}, thresholds)
	if healthy.Overall != HealthHealthy || healthy.Score == nil || *healthy.Score != 100 {
		t.Fatalf("zero error rate = %#v", healthy)
	}

	mid := healthFor(Metric{RequestCount: 50, ErrorRate: 0.10}, thresholds)
	if mid.ErrorRate != HealthWarning || mid.ErrorRateScore == nil || *mid.ErrorRateScore != 50 {
		t.Fatalf("10%% error rate = %#v", mid)
	}
	if mid.Overall != HealthWarning {
		t.Fatalf("overall = %s, want warning", mid.Overall)
	}

	p50 := int64(6500)
	ttft := healthFor(Metric{
		RequestCount: 50,
		TTFT:         LatencyStats{SampleCount: 50, P50Ms: &p50},
	}, thresholds)
	if ttft.TTFTScore == nil || *ttft.TTFTScore != 50 {
		t.Fatalf("ttft score = %#v, want 50", ttft.TTFTScore)
	}

	cache := healthFor(Metric{
		RequestCount:         50,
		CacheRate:            0,
		CacheRateDenominator: 50,
	}, thresholds)
	if cache.CacheScore == nil || *cache.CacheScore != 100 || cache.Cache != HealthHealthy {
		t.Fatalf("zero cache thresholds = %#v", cache)
	}
}
