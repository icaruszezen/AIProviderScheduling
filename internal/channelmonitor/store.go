package channelmonitor

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS metrics_1m (
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  success_requests INTEGER NOT NULL DEFAULT 0,
  error_requests INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,
  ttft_sum_ms INTEGER NOT NULL DEFAULT 0,
  ttft_count INTEGER NOT NULL DEFAULT 0,
  duration_sum_ms INTEGER NOT NULL DEFAULT 0,
  duration_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, provider, auth_index, model)
);
CREATE TABLE IF NOT EXISTS error_metrics_1m (
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  category TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  sample_message TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (bucket_start, provider, auth_index, model, category, status_code)
);
CREATE TABLE IF NOT EXISTS latency_histograms_1m (
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  kind TEXT NOT NULL,
  bucket_index INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, provider, auth_index, model, kind, bucket_index)
);
CREATE TABLE IF NOT EXISTS metrics_rollup (
  bucket_seconds INTEGER NOT NULL,
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  success_requests INTEGER NOT NULL DEFAULT 0,
  error_requests INTEGER NOT NULL DEFAULT 0,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,
  ttft_sum_ms INTEGER NOT NULL DEFAULT 0,
  ttft_count INTEGER NOT NULL DEFAULT 0,
  duration_sum_ms INTEGER NOT NULL DEFAULT 0,
  duration_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_seconds, bucket_start, provider, auth_index, model)
);
CREATE TABLE IF NOT EXISTS error_metrics_rollup (
  bucket_seconds INTEGER NOT NULL,
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  category TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  sample_message TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (bucket_seconds, bucket_start, provider, auth_index, model, category, status_code)
);
CREATE TABLE IF NOT EXISTS latency_histograms_rollup (
  bucket_seconds INTEGER NOT NULL,
  bucket_start INTEGER NOT NULL,
  provider TEXT NOT NULL,
  auth_index TEXT NOT NULL,
  model TEXT NOT NULL,
  kind TEXT NOT NULL,
  bucket_index INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_seconds, bucket_start, provider, auth_index, model, kind, bucket_index)
);
CREATE TABLE IF NOT EXISTS watermarks (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  coverage_start INTEGER NOT NULL,
  data_through INTEGER NOT NULL,
  computed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_rollup_window ON metrics_rollup (bucket_seconds, bucket_start);
CREATE INDEX IF NOT EXISTS idx_error_rollup_window ON error_metrics_rollup (bucket_seconds, bucket_start);
CREATE INDEX IF NOT EXISTS idx_hist_rollup_window ON latency_histograms_rollup (bucket_seconds, bucket_start);
CREATE INDEX IF NOT EXISTS idx_metrics_1m_series ON metrics_1m (provider, auth_index, model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_error_1m_series ON error_metrics_1m (provider, auth_index, model, bucket_start);
CREATE INDEX IF NOT EXISTS idx_hist_1m_series ON latency_histograms_1m (provider, auth_index, model, bucket_start);
`

var rollupSeconds = []int{300, 3600, 43200, 86400}

func openDatabase(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create channel monitor directory: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open channel monitor database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping channel monitor database: %w", err)
	}
	if _, err = db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create channel monitor schema: %w", err)
	}
	return db, nil
}

func sqliteDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"}
	return u.String()
}

func persistMemory(db *sql.DB, snap *memory, now time.Time) error {
	if db == nil || snap.empty() {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var minBucket, maxBucket int64
	first := true
	for key, counters := range snap.metrics {
		if err = upsertMetrics(tx, key, counters); err != nil {
			return err
		}
		if err = upsertHistograms(tx, key, counters); err != nil {
			return err
		}
		if first || key.bucket < minBucket {
			minBucket = key.bucket
		}
		if first || key.bucket > maxBucket {
			maxBucket = key.bucket
		}
		first = false
	}
	for key, sample := range snap.errors {
		if err = upsertError(tx, key, sample); err != nil {
			return err
		}
		if first || key.bucket < minBucket {
			minBucket = key.bucket
		}
		if first || key.bucket > maxBucket {
			maxBucket = key.bucket
		}
		first = false
	}
	affected := map[minuteKey]struct{}{}
	for key := range snap.metrics {
		affected[key] = struct{}{}
	}
	for key := range snap.errors {
		affected[key.minuteKey] = struct{}{}
	}
	for key := range affected {
		for _, seconds := range rollupSeconds {
			if err = recomputeRollup(tx, seconds, key); err != nil {
				return err
			}
		}
	}
	if !first {
		if err = touchWatermark(tx, minBucket, maxBucket+60, now.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func retainDatabase(db *sql.DB, now time.Time) error {
	if db == nil {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = retainLocked(tx, now); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertMetrics(tx *sql.Tx, key minuteKey, counters *bucketCounters) error {
	_, err := tx.Exec(`
INSERT INTO metrics_1m (
  bucket_start, provider, auth_index, model,
  success_requests, error_requests,
  input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(bucket_start, provider, auth_index, model) DO UPDATE SET
  success_requests = success_requests + excluded.success_requests,
  error_requests = error_requests + excluded.error_requests,
  input_tokens = input_tokens + excluded.input_tokens,
  output_tokens = output_tokens + excluded.output_tokens,
  cache_creation_tokens = cache_creation_tokens + excluded.cache_creation_tokens,
  cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
  ttft_sum_ms = ttft_sum_ms + excluded.ttft_sum_ms,
  ttft_count = ttft_count + excluded.ttft_count,
  duration_sum_ms = duration_sum_ms + excluded.duration_sum_ms,
  duration_count = duration_count + excluded.duration_count`,
		key.bucket, key.provider, key.auth, key.model,
		counters.success, counters.failed,
		counters.input, counters.output, counters.cacheCreate, counters.cacheRead,
		counters.ttftSum, counters.ttftCount, counters.durSum, counters.durCount)
	return err
}

func upsertHistograms(tx *sql.Tx, key minuteKey, counters *bucketCounters) error {
	for index, count := range counters.ttftHist {
		if err := upsertHist(tx, key, "ttft", index, count); err != nil {
			return err
		}
	}
	for index, count := range counters.durHist {
		if err := upsertHist(tx, key, "duration", index, count); err != nil {
			return err
		}
	}
	return nil
}

func upsertHist(tx *sql.Tx, key minuteKey, kind string, index int, count int64) error {
	if count == 0 {
		return nil
	}
	_, err := tx.Exec(`
INSERT INTO latency_histograms_1m (bucket_start, provider, auth_index, model, kind, bucket_index, count)
VALUES (?,?,?,?,?,?,?)
ON CONFLICT(bucket_start, provider, auth_index, model, kind, bucket_index) DO UPDATE SET
  count = count + excluded.count`,
		key.bucket, key.provider, key.auth, key.model, kind, index, count)
	return err
}

func upsertError(tx *sql.Tx, key errorKey, sample *errorSample) error {
	if sample == nil || sample.count == 0 {
		return nil
	}
	_, err := tx.Exec(`
INSERT INTO error_metrics_1m (bucket_start, provider, auth_index, model, category, status_code, count, sample_message)
VALUES (?,?,?,?,?,?,?,?)
ON CONFLICT(bucket_start, provider, auth_index, model, category, status_code) DO UPDATE SET
  count = count + excluded.count,
  sample_message = CASE WHEN error_metrics_1m.sample_message = '' THEN excluded.sample_message ELSE error_metrics_1m.sample_message END`,
		key.bucket, key.provider, key.auth, key.model, key.category, key.status, sample.count, sample.message)
	return err
}

func recomputeRollup(tx *sql.Tx, seconds int, key minuteKey) error {
	start, end := parentWindow(key.bucket, seconds)
	if _, err := tx.Exec(`DELETE FROM metrics_rollup WHERE bucket_seconds=? AND bucket_start=? AND provider=? AND auth_index=? AND model=?`,
		seconds, start, key.provider, key.auth, key.model); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO metrics_rollup (
  bucket_seconds, bucket_start, provider, auth_index, model,
  success_requests, error_requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count
)
SELECT ?, ?, provider, auth_index, model,
  COALESCE(SUM(success_requests),0), COALESCE(SUM(error_requests),0),
  COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
  COALESCE(SUM(cache_creation_tokens),0), COALESCE(SUM(cache_read_tokens),0),
  COALESCE(SUM(ttft_sum_ms),0), COALESCE(SUM(ttft_count),0),
  COALESCE(SUM(duration_sum_ms),0), COALESCE(SUM(duration_count),0)
FROM metrics_1m
WHERE provider=? AND auth_index=? AND model=? AND bucket_start>=? AND bucket_start<?
GROUP BY provider, auth_index, model`,
		seconds, start, key.provider, key.auth, key.model, start, end); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM latency_histograms_rollup WHERE bucket_seconds=? AND bucket_start=? AND provider=? AND auth_index=? AND model=?`,
		seconds, start, key.provider, key.auth, key.model); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO latency_histograms_rollup (bucket_seconds, bucket_start, provider, auth_index, model, kind, bucket_index, count)
SELECT ?, ?, provider, auth_index, model, kind, bucket_index, SUM(count)
FROM latency_histograms_1m
WHERE provider=? AND auth_index=? AND model=? AND bucket_start>=? AND bucket_start<?
GROUP BY provider, auth_index, model, kind, bucket_index`,
		seconds, start, key.provider, key.auth, key.model, start, end); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM error_metrics_rollup WHERE bucket_seconds=? AND bucket_start=? AND provider=? AND auth_index=? AND model=?`,
		seconds, start, key.provider, key.auth, key.model); err != nil {
		return err
	}
	_, err := tx.Exec(`
INSERT INTO error_metrics_rollup (bucket_seconds, bucket_start, provider, auth_index, model, category, status_code, count, sample_message)
SELECT ?, ?, provider, auth_index, model, category, status_code, SUM(count), MAX(sample_message)
FROM error_metrics_1m
WHERE provider=? AND auth_index=? AND model=? AND bucket_start>=? AND bucket_start<?
GROUP BY provider, auth_index, model, category, status_code`,
		seconds, start, key.provider, key.auth, key.model, start, end)
	return err
}

func parentWindow(minute int64, seconds int) (int64, int64) {
	size := int64(seconds)
	start := minute - minute%size
	return start, start + size
}

func touchWatermark(tx *sql.Tx, coverageStart, dataThrough, computedAt int64) error {
	_, err := tx.Exec(`
INSERT INTO watermarks (id, coverage_start, data_through, computed_at) VALUES (1, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  coverage_start = CASE
    WHEN watermarks.coverage_start = 0 OR excluded.coverage_start < watermarks.coverage_start THEN excluded.coverage_start
    ELSE watermarks.coverage_start END,
  data_through = CASE
    WHEN excluded.data_through > watermarks.data_through THEN excluded.data_through
    ELSE watermarks.data_through END,
  computed_at = excluded.computed_at`, coverageStart, dataThrough, computedAt)
	return err
}

func retainLocked(tx *sql.Tx, now time.Time) error {
	cuts := map[int]time.Duration{
		60:    7 * 24 * time.Hour,
		300:   7 * 24 * time.Hour,
		3600:  30 * 24 * time.Hour,
		43200: 45 * 24 * time.Hour,
		86400: 90 * 24 * time.Hour,
	}
	minuteCut := now.Add(-cuts[60]).Unix()
	statements := []struct {
		query string
		arg   int64
	}{
		{`DELETE FROM metrics_1m WHERE bucket_start < ?`, minuteCut},
		{`DELETE FROM error_metrics_1m WHERE bucket_start < ?`, minuteCut},
		{`DELETE FROM latency_histograms_1m WHERE bucket_start < ?`, minuteCut},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.arg); err != nil {
			return err
		}
	}
	for _, seconds := range rollupSeconds {
		cut := now.Add(-cuts[seconds]).Unix()
		for _, table := range []string{"metrics_rollup", "error_metrics_rollup", "latency_histograms_rollup"} {
			query := fmt.Sprintf(`DELETE FROM %s WHERE bucket_seconds = ? AND bucket_start < ?`, table)
			if _, err := tx.Exec(query, seconds, cut); err != nil {
				return err
			}
		}
	}
	return nil
}

type watermark struct {
	coverageStart int64
	dataThrough   int64
	computedAt    int64
}

func loadWatermark(db *sql.DB) (watermark, error) {
	var mark watermark
	if db == nil {
		return mark, nil
	}
	err := db.QueryRow(`SELECT coverage_start, data_through, computed_at FROM watermarks WHERE id=1`).Scan(
		&mark.coverageStart, &mark.dataThrough, &mark.computedAt)
	if err == sql.ErrNoRows {
		return watermark{}, nil
	}
	return mark, err
}

func loadRollup(db *sql.DB, bucketSeconds int, start, end int64) (*memory, error) {
	out := newMemory()
	if db == nil {
		return out, nil
	}
	rows, err := db.Query(`
SELECT provider, auth_index, model, bucket_start,
  success_requests, error_requests, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
  ttft_sum_ms, ttft_count, duration_sum_ms, duration_count
FROM metrics_rollup
WHERE bucket_seconds=? AND bucket_start>=? AND bucket_start<?`, bucketSeconds, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key minuteKey
		counters := &bucketCounters{}
		if err = rows.Scan(&key.provider, &key.auth, &key.model, &key.bucket,
			&counters.success, &counters.failed, &counters.input, &counters.output, &counters.cacheCreate, &counters.cacheRead,
			&counters.ttftSum, &counters.ttftCount, &counters.durSum, &counters.durCount); err != nil {
			return nil, err
		}
		out.metrics[key] = counters
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	histRows, err := db.Query(`
SELECT provider, auth_index, model, bucket_start, kind, bucket_index, count
FROM latency_histograms_rollup
WHERE bucket_seconds=? AND bucket_start>=? AND bucket_start<?`, bucketSeconds, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = histRows.Close() }()
	for histRows.Next() {
		var key minuteKey
		var kind string
		var index int
		var count int64
		if err = histRows.Scan(&key.provider, &key.auth, &key.model, &key.bucket, &kind, &index, &count); err != nil {
			return nil, err
		}
		if index < 0 || index >= histBuckets {
			continue
		}
		counters := out.metrics[key]
		if counters == nil {
			counters = &bucketCounters{}
			out.metrics[key] = counters
		}
		if kind == "duration" {
			counters.durHist[index] += count
		} else {
			counters.ttftHist[index] += count
		}
	}
	if err = histRows.Err(); err != nil {
		return nil, err
	}
	errRows, err := db.Query(`
SELECT provider, auth_index, model, bucket_start, category, status_code, count, sample_message
FROM error_metrics_rollup
WHERE bucket_seconds=? AND bucket_start>=? AND bucket_start<?`, bucketSeconds, start, end)
	if err != nil {
		return nil, err
	}
	defer func() { _ = errRows.Close() }()
	for errRows.Next() {
		var key errorKey
		var count int64
		var message string
		if err = errRows.Scan(&key.provider, &key.auth, &key.model, &key.bucket, &key.category, &key.status, &count, &message); err != nil {
			return nil, err
		}
		out.errors[key] = &errorSample{count: count, message: message}
		counters := out.metrics[key.minuteKey]
		if counters == nil {
			counters = &bucketCounters{}
			out.metrics[key.minuteKey] = counters
		}
		if counters.errByCat == nil {
			counters.errByCat = map[string]int64{}
		}
		counters.errByCat[key.category] += count
	}
	return out, errRows.Err()
}
