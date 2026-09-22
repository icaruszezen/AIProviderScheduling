package channelmonitor

import "time"

// Query filters a monitor read. Empty lists mean no filter.
type Query struct {
	Range       string
	Providers   []string
	AuthIndexes []string
	Models      []string
}

// LatencyStats is a percentile summary over one window.
type LatencyStats struct {
	SampleCount int64  `json:"sample_count"`
	P50Ms       *int64 `json:"p50_ms,omitempty"`
	P90Ms       *int64 `json:"p90_ms,omitempty"`
	P95Ms       *int64 `json:"p95_ms,omitempty"`
	AvgMs       *int64 `json:"avg_ms,omitempty"`
}

// Metric is the derived view of one aggregate.
type Metric struct {
	SuccessRequests      int64        `json:"success_requests"`
	ErrorRequests        int64        `json:"error_requests"`
	RequestCount         int64        `json:"request_count"`
	InputTokens          int64        `json:"input_tokens"`
	OutputTokens         int64        `json:"output_tokens"`
	CacheCreationTokens  int64        `json:"cache_creation_tokens"`
	CacheReadTokens      int64        `json:"cache_read_tokens"`
	TokenCount           int64        `json:"token_count"`
	RPM                  float64      `json:"rpm"`
	TPM                  float64      `json:"tpm"`
	ErrorRate            float64      `json:"error_rate"`
	SuccessRate          float64      `json:"success_rate"`
	CacheRate            float64      `json:"cache_rate"`
	CacheRateNumerator   int64        `json:"cache_rate_numerator"`
	CacheRateDenominator int64        `json:"cache_rate_denominator"`
	TTFT                 LatencyStats `json:"ttft"`
	Duration             LatencyStats `json:"duration"`
}

// Coverage describes how much of the requested window has been aggregated.
type Coverage struct {
	RequestedStart        time.Time `json:"requested_start"`
	RequestedEnd          time.Time `json:"requested_end"`
	CoverageStart         time.Time `json:"coverage_start"`
	DataThrough           time.Time `json:"data_through"`
	ComputedAt            time.Time `json:"computed_at"`
	AggregationLagSeconds int64     `json:"aggregation_lag_seconds"`
	CoverageComplete      bool      `json:"coverage_complete"`
	BucketSeconds         int       `json:"bucket_seconds"`
}

// TrendPoint is one display bucket.
type TrendPoint struct {
	BucketStart time.Time `json:"bucket_start"`
	Metrics     Metric    `json:"metrics"`
	Health      Health    `json:"health"`
}

// SummaryItem is the workbench thumbnail for one credential.
type SummaryItem struct {
	Provider  string       `json:"provider"`
	AuthIndex string       `json:"auth_index"`
	Metrics   Metric       `json:"metrics"`
	Health    Health       `json:"health"`
	Buckets   []TrendPoint `json:"buckets"`
}

// SummaryResponse lists credentials that have traffic in the window.
type SummaryResponse struct {
	Enabled  bool          `json:"enabled"`
	Range    string        `json:"range"`
	Coverage Coverage      `json:"coverage"`
	Items    []SummaryItem `json:"items"`
}

// SnapshotResponse is the detail view for the current filters.
type SnapshotResponse struct {
	Enabled  bool         `json:"enabled"`
	Range    string       `json:"range"`
	Coverage Coverage     `json:"coverage"`
	Metrics  Metric       `json:"metrics"`
	Health   Health       `json:"health"`
	Trend    []TrendPoint `json:"trend"`
}

// ModelItem is one provider/auth/model row.
type ModelItem struct {
	Provider  string `json:"provider"`
	AuthIndex string `json:"auth_index"`
	Model     string `json:"model"`
	Metrics   Metric `json:"metrics"`
	Health    Health `json:"health"`
}

// ModelsResponse lists models inside the filter.
type ModelsResponse struct {
	Enabled  bool        `json:"enabled"`
	Range    string      `json:"range"`
	Coverage Coverage    `json:"coverage"`
	Items    []ModelItem `json:"items"`
}

// ErrorDetail is one repeated upstream failure sample.
type ErrorDetail struct {
	Provider   string `json:"provider"`
	AuthIndex  string `json:"auth_index"`
	Model      string `json:"model"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message,omitempty"`
	Count      int64  `json:"count"`
}

// ErrorItem groups failures by taxonomy category.
type ErrorItem struct {
	Category string        `json:"category"`
	Count    int64         `json:"count"`
	Rate     float64       `json:"rate"`
	Ignored  bool          `json:"ignored"`
	Details  []ErrorDetail `json:"details,omitempty"`
}

// ErrorsResponse is the error taxonomy for the current filter.
type ErrorsResponse struct {
	Enabled  bool        `json:"enabled"`
	Range    string      `json:"range"`
	Coverage Coverage    `json:"coverage"`
	Items    []ErrorItem `json:"items"`
}
