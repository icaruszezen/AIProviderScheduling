package channelmonitor

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type rangeWindow struct {
	name   string
	start  time.Time
	end    time.Time
	bucket int
}

func (s *Service) Summaries(q Query) (SummaryResponse, error) {
	data, win, cfg, coverage, err := s.collect(q)
	if err != nil {
		return SummaryResponse{}, err
	}
	response := SummaryResponse{
		Enabled:  cfg.Enabled,
		Range:    win.name,
		Coverage: coverage,
		Items:    []SummaryItem{},
	}
	grouped := map[credKey]map[int64]*bucketCounters{}
	for key, counters := range data.metrics {
		cred := credKey{provider: key.provider, auth: key.auth}
		buckets := grouped[cred]
		if buckets == nil {
			buckets = map[int64]*bucketCounters{}
			grouped[cred] = buckets
		}
		if buckets[key.bucket] == nil {
			buckets[key.bucket] = &bucketCounters{}
		}
		buckets[key.bucket].add(counters)
	}
	creds := make([]credKey, 0, len(grouped))
	for cred := range grouped {
		creds = append(creds, cred)
	}
	sort.Slice(creds, func(i, j int) bool {
		if creds[i].provider != creds[j].provider {
			return creds[i].provider < creds[j].provider
		}
		return creds[i].auth < creds[j].auth
	})
	ignored := ignoredSet(cfg.IgnoredErrorCategories)
	for _, cred := range creds {
		total := &bucketCounters{}
		for _, counters := range grouped[cred] {
			total.add(counters)
		}
		if total.success+total.failed == 0 {
			continue
		}
		metrics := total.metric(int(win.end.Sub(win.start).Seconds()), ignored)
		item := SummaryItem{
			Provider:  cred.provider,
			AuthIndex: cred.auth,
			Metrics:   metrics,
			Health:    healthFor(metrics, cfg.HealthThresholds),
			Buckets:   seriesPoints(win, grouped[cred], ignored, cfg.HealthThresholds),
		}
		response.Items = append(response.Items, item)
	}
	return response, nil
}

func (s *Service) Snapshot(q Query) (SnapshotResponse, error) {
	data, win, cfg, coverage, err := s.collect(q)
	if err != nil {
		return SnapshotResponse{}, err
	}
	byBucket := map[int64]*bucketCounters{}
	total := &bucketCounters{}
	for key, counters := range data.metrics {
		if byBucket[key.bucket] == nil {
			byBucket[key.bucket] = &bucketCounters{}
		}
		byBucket[key.bucket].add(counters)
		total.add(counters)
	}
	ignored := ignoredSet(cfg.IgnoredErrorCategories)
	metrics := total.metric(int(win.end.Sub(win.start).Seconds()), ignored)
	return SnapshotResponse{
		Enabled:  cfg.Enabled,
		Range:    win.name,
		Coverage: coverage,
		Metrics:  metrics,
		Health:   healthFor(metrics, cfg.HealthThresholds),
		Trend:    seriesPoints(win, byBucket, ignored, cfg.HealthThresholds),
	}, nil
}

func (s *Service) Models(q Query) (ModelsResponse, error) {
	data, win, cfg, coverage, err := s.collect(q)
	if err != nil {
		return ModelsResponse{}, err
	}
	grouped := map[modelKey]*bucketCounters{}
	for key, counters := range data.metrics {
		model := modelKey{provider: key.provider, auth: key.auth, model: key.model}
		if grouped[model] == nil {
			grouped[model] = &bucketCounters{}
		}
		grouped[model].add(counters)
	}
	keys := make([]modelKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := grouped[keys[i]].success + grouped[keys[i]].failed
		right := grouped[keys[j]].success + grouped[keys[j]].failed
		if left != right {
			return left > right
		}
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		if keys[i].provider != keys[j].provider {
			return keys[i].provider < keys[j].provider
		}
		return keys[i].auth < keys[j].auth
	})
	ignored := ignoredSet(cfg.IgnoredErrorCategories)
	response := ModelsResponse{
		Enabled:  cfg.Enabled,
		Range:    win.name,
		Coverage: coverage,
		Items:    []ModelItem{},
	}
	windowSeconds := int(win.end.Sub(win.start).Seconds())
	for _, key := range keys {
		if grouped[key].success+grouped[key].failed == 0 {
			continue
		}
		metrics := grouped[key].metric(windowSeconds, ignored)
		response.Items = append(response.Items, ModelItem{
			Provider:  key.provider,
			AuthIndex: key.auth,
			Model:     key.model,
			Metrics:   metrics,
			Health:    healthFor(metrics, cfg.HealthThresholds),
		})
	}
	return response, nil
}

func (s *Service) Errors(q Query) (ErrorsResponse, error) {
	data, win, cfg, coverage, err := s.collect(q)
	if err != nil {
		return ErrorsResponse{}, err
	}
	type detailKey struct {
		provider string
		auth     string
		model    string
		status   int
		message  string
	}
	type categoryGroup struct {
		count   int64
		details map[detailKey]int64
	}
	groups := map[string]*categoryGroup{}
	var total int64
	for key, sample := range data.errors {
		if sample == nil || sample.count == 0 {
			continue
		}
		group := groups[key.category]
		if group == nil {
			group = &categoryGroup{details: map[detailKey]int64{}}
			groups[key.category] = group
		}
		group.count += sample.count
		total += sample.count
		detail := detailKey{provider: key.provider, auth: key.auth, model: key.model, status: key.status, message: sample.message}
		group.details[detail] += sample.count
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if groups[names[i]].count != groups[names[j]].count {
			return groups[names[i]].count > groups[names[j]].count
		}
		return names[i] < names[j]
	})
	ignored := ignoredSet(cfg.IgnoredErrorCategories)
	response := ErrorsResponse{
		Enabled:  cfg.Enabled,
		Range:    win.name,
		Coverage: coverage,
		Items:    []ErrorItem{},
	}
	for _, name := range names {
		group := groups[name]
		item := ErrorItem{Category: name, Count: group.count, Ignored: false}
		if _, ok := ignored[name]; ok {
			item.Ignored = true
		}
		if total > 0 {
			item.Rate = float64(group.count) / float64(total)
		}
		details := make([]ErrorDetail, 0, len(group.details))
		for detail, count := range group.details {
			details = append(details, ErrorDetail{
				Provider:   detail.provider,
				AuthIndex:  detail.auth,
				Model:      detail.model,
				StatusCode: detail.status,
				Message:    detail.message,
				Count:      count,
			})
		}
		sort.Slice(details, func(i, j int) bool {
			if details[i].Count != details[j].Count {
				return details[i].Count > details[j].Count
			}
			if details[i].Model != details[j].Model {
				return details[i].Model < details[j].Model
			}
			return details[i].StatusCode < details[j].StatusCode
		})
		item.Details = details
		response.Items = append(response.Items, item)
	}
	return response, nil
}

type credKey struct {
	provider string
	auth     string
}

type modelKey struct {
	provider string
	auth     string
	model    string
}

func (s *Service) collect(q Query) (*memory, rangeWindow, config.ChannelMonitorConfig, Coverage, error) {
	if s == nil {
		return newMemory(), rangeWindow{}, config.ChannelMonitorConfig{}, Coverage{}, nil
	}
	// Retry when a flush commits between the memory clone and the SQLite read.
	// A stable flushSeq means the clone is still absent from the database.
	for attempt := 0; attempt < 4; attempt++ {
		s.mu.Lock()
		seq := s.flushSeq
		pending := s.mem.clone()
		cfg := s.cfg
		now := s.currentTimeLocked()
		s.mu.Unlock()
		if attempt == 0 && s.beforeCollectLoad != nil {
			hook := s.beforeCollectLoad
			s.beforeCollectLoad = nil
			hook()
		}
		win := parseRange(now, q.Range)
		s.mu.Lock()
		db := s.db
		if db != nil {
			s.dbMu.RLock()
		}
		s.mu.Unlock()
		stored, mark, err := readStored(db, win.bucket, win.start.Unix(), win.end.Unix())
		if db != nil {
			s.dbMu.RUnlock()
		}
		if err != nil {
			return nil, win, config.ChannelMonitorConfig{}, Coverage{}, err
		}
		s.mu.Lock()
		stable := s.flushSeq == seq
		if !stable && attempt == 3 {
			pending = s.mem.clone()
			stable = true
		}
		s.mu.Unlock()
		if !stable {
			continue
		}
		return combineMemory(stored, pending, q, win), win, cfg, coverageFrom(win, mark, pending, now), nil
	}
	return nil, rangeWindow{}, config.ChannelMonitorConfig{}, Coverage{}, fmt.Errorf("channel monitor query conflicted with flush")
}

func readStored(db *sql.DB, bucket int, start, end int64) (*memory, watermark, error) {
	if db == nil {
		return newMemory(), watermark{}, nil
	}
	stored, err := loadRollup(db, bucket, start, end)
	if err != nil {
		return nil, watermark{}, err
	}
	mark, err := loadWatermark(db)
	if err != nil {
		return nil, watermark{}, err
	}
	return stored, mark, nil
}

func combineMemory(stored, pending *memory, q Query, win rangeWindow) *memory {
	combined := newMemory()
	if stored != nil {
		for key, counters := range stored.metrics {
			if !matches(q, key.provider, key.auth, key.model) {
				continue
			}
			combined.metrics[key] = counters.clone()
		}
		for key, sample := range stored.errors {
			if sample == nil || !matches(q, key.provider, key.auth, key.model) {
				continue
			}
			copied := *sample
			combined.errors[key] = &copied
		}
	}
	if pending == nil {
		return combined
	}
	for key, counters := range pending.metrics {
		if key.bucket < win.start.Unix() || key.bucket >= win.end.Unix() {
			continue
		}
		if !matches(q, key.provider, key.auth, key.model) {
			continue
		}
		display, _ := parentWindow(key.bucket, win.bucket)
		folded := minuteKey{bucket: display, provider: key.provider, auth: key.auth, model: key.model}
		if combined.metrics[folded] == nil {
			combined.metrics[folded] = counters.clone()
			continue
		}
		combined.metrics[folded].add(counters)
	}
	for key, sample := range pending.errors {
		if sample == nil || key.bucket < win.start.Unix() || key.bucket >= win.end.Unix() {
			continue
		}
		if !matches(q, key.provider, key.auth, key.model) {
			continue
		}
		display, _ := parentWindow(key.bucket, win.bucket)
		folded := key
		folded.bucket = display
		current := combined.errors[folded]
		if current == nil {
			copied := *sample
			combined.errors[folded] = &copied
			continue
		}
		current.count += sample.count
		if current.message == "" {
			current.message = sample.message
		}
	}
	return combined
}

func coverageFrom(win rangeWindow, mark watermark, pending *memory, now time.Time) Coverage {
	nowUnix := now.UTC().Unix()
	coverageStart := mark.coverageStart
	dataThrough := mark.dataThrough
	consider := func(bucket int64) {
		if bucket <= 0 {
			return
		}
		if coverageStart == 0 || bucket < coverageStart {
			coverageStart = bucket
		}
		end := bucket + 60
		if end > dataThrough {
			dataThrough = end
		}
	}
	if pending != nil {
		for key := range pending.metrics {
			consider(key.bucket)
		}
		for key := range pending.errors {
			consider(key.bucket)
		}
	}
	if dataThrough > nowUnix {
		dataThrough = nowUnix
	}
	coverage := Coverage{
		RequestedStart: win.start,
		RequestedEnd:   win.end,
		ComputedAt:     now.UTC(),
		BucketSeconds:  win.bucket,
	}
	if coverageStart > 0 {
		coverage.CoverageStart = time.Unix(coverageStart, 0).UTC()
		coverage.CoverageComplete = !coverage.CoverageStart.After(win.start)
	}
	if dataThrough > 0 {
		coverage.DataThrough = time.Unix(dataThrough, 0).UTC()
		lag := nowUnix - dataThrough
		if lag > 0 {
			coverage.AggregationLagSeconds = lag
		}
	}
	return coverage
}

func parseRange(now time.Time, name string) rangeWindow {
	var window time.Duration
	var bucket int
	canonical := "90m"
	switch strings.TrimSpace(name) {
	case "24h":
		window, bucket, canonical = 24*time.Hour, 3600, "24h"
	case "7d":
		window, bucket, canonical = 7*24*time.Hour, 43200, "7d"
	case "30d":
		window, bucket, canonical = 30*24*time.Hour, 86400, "30d"
	default:
		window, bucket, canonical = 90*time.Minute, 300, "90m"
	}
	nowUnix := now.UTC().Unix()
	size := int64(bucket)
	endUnix := nowUnix - nowUnix%size + size
	end := time.Unix(endUnix, 0).UTC()
	return rangeWindow{name: canonical, start: end.Add(-window), end: end, bucket: bucket}
}

func seriesPoints(win rangeWindow, buckets map[int64]*bucketCounters, ignored map[string]struct{}, thresholds config.ChannelMonitorHealthThresholds) []TrendPoint {
	points := make([]TrendPoint, 0, int(win.end.Sub(win.start).Seconds())/win.bucket)
	for ts := win.start.Unix(); ts < win.end.Unix(); ts += int64(win.bucket) {
		counters := buckets[ts]
		if counters == nil {
			counters = &bucketCounters{}
		}
		metrics := counters.metric(win.bucket, ignored)
		points = append(points, TrendPoint{
			BucketStart: time.Unix(ts, 0).UTC(),
			Metrics:     metrics,
			Health:      healthFor(metrics, thresholds),
		})
	}
	return points
}

func matches(q Query, provider, auth, model string) bool {
	return matchList(q.Providers, provider) && matchList(q.AuthIndexes, auth) && matchList(q.Models, model)
}
