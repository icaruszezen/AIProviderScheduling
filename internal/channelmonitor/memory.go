package channelmonitor

import (
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type minuteKey struct {
	bucket   int64
	provider string
	auth     string
	model    string
}

type pointKey struct {
	bucket   int64
	provider string
	auth     string
	model    string
}

type errorKey struct {
	minuteKey
	category string
	status   int
}

type errorSample struct {
	count   int64
	message string
}

type bucketCounters struct {
	success     int64
	failed      int64
	input       int64
	output      int64
	cacheCreate int64
	cacheRead   int64
	ttftSum     int64
	ttftCount   int64
	durSum      int64
	durCount    int64
	ttftHist    [histBuckets]int64
	durHist     [histBuckets]int64
	errByCat    map[string]int64
}

type memory struct {
	metrics map[minuteKey]*bucketCounters
	errors  map[errorKey]*errorSample
}

func newMemory() *memory {
	return &memory{
		metrics: map[minuteKey]*bucketCounters{},
		errors:  map[errorKey]*errorSample{},
	}
}

func (m *memory) empty() bool {
	return m == nil || (len(m.metrics) == 0 && len(m.errors) == 0)
}

func (m *memory) size() int {
	if m == nil {
		return 0
	}
	return len(m.metrics) + len(m.errors)
}

func (m *memory) requestCount() int64 {
	if m == nil {
		return 0
	}
	var count int64
	for _, counters := range m.metrics {
		if counters == nil {
			continue
		}
		count += counters.success + counters.failed
	}
	return count
}

func (m *memory) add(now time.Time, record usage.Record) {
	if m.metrics == nil {
		m.metrics = map[minuteKey]*bucketCounters{}
	}
	if m.errors == nil {
		m.errors = map[errorKey]*errorSample{}
	}
	at := record.RequestedAt
	if at.IsZero() {
		at = now
	}
	provider := strings.TrimSpace(record.Provider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "unknown"
	}
	key := minuteKey{
		bucket:   at.UTC().Unix() / 60 * 60,
		provider: provider,
		auth:     strings.TrimSpace(record.AuthIndex),
		model:    model,
	}
	counters := m.metrics[key]
	if counters == nil {
		counters = &bucketCounters{}
		m.metrics[key] = counters
	}
	if record.Failed {
		counters.failed++
		category := Classify(ErrorInput{StatusCode: record.Fail.StatusCode, Message: record.Fail.Body})
		if counters.errByCat == nil {
			counters.errByCat = map[string]int64{}
		}
		counters.errByCat[category]++
		errKey := errorKey{minuteKey: key, category: category, status: record.Fail.StatusCode}
		sample := m.errors[errKey]
		if sample == nil {
			sample = &errorSample{message: truncateRunes(record.Fail.Body, 300)}
			m.errors[errKey] = sample
		}
		sample.count++
	} else {
		counters.success++
	}
	detail := record.Detail
	counters.input += detail.InputTokens
	counters.output += detail.OutputTokens
	counters.cacheCreate += detail.CacheCreationTokens
	cacheRead := detail.CacheReadTokens
	if cacheRead == 0 {
		cacheRead = detail.CachedTokens
	}
	counters.cacheRead += cacheRead
	if record.TTFT > 0 {
		ms := record.TTFT.Milliseconds()
		counters.ttftSum += ms
		counters.ttftCount++
		counters.ttftHist[histogramIndex(ms)]++
	}
	if record.Latency > 0 {
		ms := record.Latency.Milliseconds()
		counters.durSum += ms
		counters.durCount++
		counters.durHist[histogramIndex(ms)]++
	}
}

func (c *bucketCounters) clone() *bucketCounters {
	if c == nil {
		return &bucketCounters{}
	}
	out := *c
	if len(c.errByCat) > 0 {
		out.errByCat = make(map[string]int64, len(c.errByCat))
		for key, value := range c.errByCat {
			out.errByCat[key] = value
		}
	}
	return &out
}

func (c *bucketCounters) add(other *bucketCounters) {
	if c == nil || other == nil {
		return
	}
	c.success += other.success
	c.failed += other.failed
	c.input += other.input
	c.output += other.output
	c.cacheCreate += other.cacheCreate
	c.cacheRead += other.cacheRead
	c.ttftSum += other.ttftSum
	c.ttftCount += other.ttftCount
	c.durSum += other.durSum
	c.durCount += other.durCount
	for i := range c.ttftHist {
		c.ttftHist[i] += other.ttftHist[i]
		c.durHist[i] += other.durHist[i]
	}
	for key, value := range other.errByCat {
		if c.errByCat == nil {
			c.errByCat = map[string]int64{}
		}
		c.errByCat[key] += value
	}
}

func (m *memory) clone() *memory {
	out := newMemory()
	if m == nil {
		return out
	}
	for key, counters := range m.metrics {
		out.metrics[key] = counters.clone()
	}
	for key, sample := range m.errors {
		copied := *sample
		out.errors[key] = &copied
	}
	return out
}

func (m *memory) merge(other *memory) {
	if m == nil || other == nil {
		return
	}
	for key, counters := range other.metrics {
		current := m.metrics[key]
		if current == nil {
			m.metrics[key] = counters.clone()
			continue
		}
		current.add(counters)
	}
	for key, sample := range other.errors {
		current := m.errors[key]
		if current == nil {
			copied := *sample
			m.errors[key] = &copied
			continue
		}
		current.count += sample.count
		if current.message == "" {
			current.message = sample.message
		}
	}
}

func (c *bucketCounters) metric(windowSeconds int, ignored map[string]struct{}) Metric {
	if c == nil {
		c = &bucketCounters{}
	}
	var ignoredCount int64
	for category, count := range c.errByCat {
		if _, ok := ignored[category]; ok {
			ignoredCount += count
		}
	}
	adjustedErrors := c.failed - ignoredCount
	if adjustedErrors < 0 {
		adjustedErrors = 0
	}
	requests := c.success + c.failed
	minutes := float64(windowSeconds) / 60
	if minutes <= 0 {
		minutes = 1
	}
	tokenCount := c.input + c.output + c.cacheCreate + c.cacheRead
	metric := Metric{
		SuccessRequests:      c.success,
		ErrorRequests:        c.failed,
		RequestCount:         requests,
		InputTokens:          c.input,
		OutputTokens:         c.output,
		CacheCreationTokens:  c.cacheCreate,
		CacheReadTokens:      c.cacheRead,
		TokenCount:           tokenCount,
		RPM:                  float64(requests) / minutes,
		TPM:                  float64(tokenCount) / minutes,
		CacheRateNumerator:   c.cacheRead,
		CacheRateDenominator: c.input + c.cacheCreate + c.cacheRead,
		TTFT:                 latencyStats(c.ttftHist, c.ttftSum, c.ttftCount),
		Duration:             latencyStats(c.durHist, c.durSum, c.durCount),
	}
	if requests > 0 {
		metric.ErrorRate = float64(adjustedErrors) / float64(requests)
		metric.SuccessRate = float64(c.success) / float64(requests)
	}
	if metric.CacheRateDenominator > 0 {
		metric.CacheRate = float64(metric.CacheRateNumerator) / float64(metric.CacheRateDenominator)
	}
	return metric
}

func latencyStats(hist [histBuckets]int64, sum, count int64) LatencyStats {
	stats := LatencyStats{SampleCount: count, P50Ms: percentile(hist, 0.50), P90Ms: percentile(hist, 0.90), P95Ms: percentile(hist, 0.95)}
	if count > 0 {
		avg := sum / count
		stats.AvgMs = &avg
	}
	return stats
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
