package channelmonitor

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const pluginName = "channel-monitor"

// maxPendingKeys bounds the in-memory buffer while the database is unavailable.
const maxPendingKeys = 8192

// Service aggregates usage records into a node-local SQLite database.
type Service struct {
	mu         sync.Mutex
	flushMu    sync.Mutex
	dbMu       sync.RWMutex
	configPath string
	cfg        config.ChannelMonitorConfig
	db         *sql.DB
	dbPath     string
	mem        *memory
	flushSeq   uint64
	lastRetain time.Time
	ingest     chan usage.Record
	stop       chan struct{}
	done       chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	running    atomic.Bool
	closed     atomic.Bool
	dropped    atomic.Int64
	now        func() time.Time
	// beforeCollectLoad, when set, runs after a query clones pending memory and
	// before it reads SQLite. Tests use it to commit a flush inside that window.
	beforeCollectLoad func()
}

// New builds a monitor. Start registers it on the usage pipeline.
func New(configPath string, cfg config.ChannelMonitorConfig) *Service {
	service := &Service{
		configPath: configPath,
		mem:        newMemory(),
		ingest:     make(chan usage.Record, 4096),
		stop:       make(chan struct{}),
		now:        time.Now,
	}
	service.cfg = Effective(cfg)
	return service
}

// Start begins ingestion. It is safe to call once.
func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.startOnce.Do(func() {
		usage.RegisterNamedPlugin(pluginName, s)
		s.done = make(chan struct{})
		s.running.Store(true)
		s.flushMu.Lock()
		s.mu.Lock()
		s.ensureDBLocked()
		s.mu.Unlock()
		s.flushMu.Unlock()
		go s.loop()
	})
}

// Stop flushes pending samples and closes the database.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.closed.Store(true)
		s.running.Store(false)
		if s.done != nil {
			close(s.stop)
			<-s.done
		}
		_ = s.Flush()
		s.flushMu.Lock()
		s.mu.Lock()
		old := s.db
		s.db = nil
		s.dbPath = ""
		s.mu.Unlock()
		if old != nil {
			s.dbMu.Lock()
			if err := old.Close(); err != nil {
				log.WithError(err).Warn("channel monitor: close database")
			}
			s.dbMu.Unlock()
		}
		s.flushMu.Unlock()
	})
}

// ApplyConfig updates filters and reopens the database when the path changes.
func (s *Service) ApplyConfig(cfg config.ChannelMonitorConfig) {
	if s == nil {
		return
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	_ = s.flushLocked()
	s.mu.Lock()
	s.cfg = Effective(cfg)
	s.ensureDBLocked()
	s.mu.Unlock()
}

// HandleUsage accepts one usage record without blocking the caller.
func (s *Service) HandleUsage(_ context.Context, record usage.Record) {
	if s == nil || !s.running.Load() || !s.allows(record) {
		return
	}
	select {
	case s.ingest <- record:
	default:
		dropped := s.dropped.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			log.WithField("dropped", dropped).Warn("channel monitor ingest queue full; dropping usage records")
		}
	}
}

// Flush writes buffered samples to SQLite.
func (s *Service) Flush() error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.flushLocked()
}

// flushLocked persists the current buffer. The caller must hold flushMu.
func (s *Service) flushLocked() error {
	s.mu.Lock()
	snap := s.mem
	s.mem = newMemory()
	if !snap.empty() {
		s.flushSeq++
	}
	if s.db == nil {
		s.ensureDBLocked()
	}
	db := s.db
	now := s.currentTimeLocked()
	retain := db != nil && (s.lastRetain.IsZero() || now.Sub(s.lastRetain) >= time.Hour)
	if db != nil {
		s.dbMu.RLock()
	}
	s.mu.Unlock()
	if db != nil {
		defer s.dbMu.RUnlock()
	}
	if !snap.empty() {
		if db == nil {
			s.mu.Lock()
			s.mergePendingLocked(snap)
			s.mu.Unlock()
			return nil
		}
		if err := persistMemory(db, snap, now); err != nil {
			log.WithError(err).Warn("channel monitor: flush failed")
			s.mu.Lock()
			s.mergePendingLocked(snap)
			s.mu.Unlock()
			return err
		}
	}
	if retain {
		if err := retainDatabase(db, now); err != nil {
			log.WithError(err).Warn("channel monitor: retain old buckets")
		}
		s.mu.Lock()
		s.lastRetain = now
		s.mu.Unlock()
	}
	return nil
}

// mergePendingLocked keeps a failed batch only while the buffer is under the cap.
// The caller must hold mu.
func (s *Service) mergePendingLocked(snap *memory) {
	if snap == nil || snap.empty() {
		return
	}
	if s.mem.size()+snap.size() > maxPendingKeys {
		dropped := s.dropped.Add(snap.requestCount())
		log.WithField("dropped", dropped).Error("channel monitor: dropping samples; database is unavailable and the memory buffer is full")
		return
	}
	s.mem.merge(snap)
}

func (s *Service) allows(record usage.Record) bool {
	if !usage.GenerateEnabled(record.Generate) {
		return false
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if !cfg.Enabled {
		return false
	}
	provider := strings.TrimSpace(record.Provider)
	model := strings.TrimSpace(record.Model)
	authIndex := strings.TrimSpace(record.AuthIndex)
	return matchList(cfg.Providers, provider) && matchList(cfg.AuthIndexes, authIndex) && matchList(cfg.Models, model)
}

// ObserveForTest records one sample without starting the background loop.
// When the record has RequestedAt, later reads use that instant as "now" so the
// sample stays inside the selected window.
func (s *Service) ObserveForTest(record usage.Record) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !record.RequestedAt.IsZero() {
		at := record.RequestedAt
		s.now = func() time.Time { return at }
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.mem.add(s.currentTimeLocked(), record)
}

func (s *Service) addRecord(record usage.Record) {
	if !s.allows(record) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil && s.mem.size() >= maxPendingKeys {
		dropped := s.dropped.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			log.WithField("dropped", dropped).Error("channel monitor: dropping usage record; database is unavailable and the memory buffer is full")
		}
		return
	}
	s.mem.add(s.currentTimeLocked(), record)
}

func (s *Service) loop() {
	defer close(s.done)
	timer := time.NewTimer(s.flushEvery())
	defer timer.Stop()
	for {
		select {
		case <-s.stop:
			s.drain()
			return
		case record := <-s.ingest:
			s.addRecord(record)
		case <-timer.C:
			_ = s.Flush()
			timer.Reset(s.flushEvery())
		}
	}
}

func (s *Service) drain() {
	for {
		select {
		case record := <-s.ingest:
			s.addRecord(record)
		default:
			return
		}
	}
}

func (s *Service) currentTimeLocked() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *Service) flushEvery() time.Duration {
	s.mu.Lock()
	seconds := s.cfg.RefreshIntervalSeconds
	s.mu.Unlock()
	if seconds != 60 && seconds != 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

// ensureDBLocked opens or swaps the database. The caller holds mu and flushMu.
// It may drop mu while closing the previous database, and it re-locks before returning.
func (s *Service) ensureDBLocked() {
	if s.closed.Load() || (!s.cfg.Enabled && s.db == nil) {
		return
	}
	path := ResolveDatabasePath(s.configPath, s.cfg.DatabasePath)
	if s.db != nil && path == s.dbPath {
		return
	}
	if s.db != nil && !s.cfg.Enabled {
		return
	}
	if s.db != nil {
		old := s.db
		s.db = nil
		s.dbPath = ""
		s.mu.Unlock()
		s.dbMu.Lock()
		if err := old.Close(); err != nil {
			log.WithError(err).Warn("channel monitor: close previous database")
		}
		s.dbMu.Unlock()
		s.mu.Lock()
		if s.closed.Load() || !s.cfg.Enabled {
			return
		}
		path = ResolveDatabasePath(s.configPath, s.cfg.DatabasePath)
	}
	if !s.cfg.Enabled {
		return
	}
	db, err := openDatabase(path)
	if err != nil {
		log.WithError(err).Error("channel monitor: open database")
		return
	}
	s.db = db
	s.dbPath = path
}

// ResolveDatabasePath picks the sqlite file. An empty configured path uses
// channel-monitor.db beside the config file.
func ResolveDatabasePath(configPath, configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	dir := "."
	if strings.TrimSpace(configPath) != "" {
		dir = filepath.Dir(configPath)
	}
	return filepath.Join(dir, "channel-monitor.db")
}

func matchList(list []string, value string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

// Effective applies runtime defaults without persisting them.
func Effective(cfg config.ChannelMonitorConfig) config.ChannelMonitorConfig {
	cfg = cfg.Normalized()
	if cfg.RefreshIntervalSeconds == 0 {
		cfg.RefreshIntervalSeconds = 300
	}
	if cfg.IgnoredErrorCategories == nil {
		cfg.IgnoredErrorCategories = append([]string{}, DefaultIgnoredCategories...)
	} else {
		cfg.IgnoredErrorCategories = filterKnownCategories(cfg.IgnoredErrorCategories)
	}
	cfg.HealthThresholds = normalizeThresholds(cfg.HealthThresholds)
	return cfg
}

func filterKnownCategories(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !knownCategory(value) {
			noteUnknownCategory(value)
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

var notedUnknownCategories sync.Map

func noteUnknownCategory(value string) {
	if _, loaded := notedUnknownCategories.LoadOrStore(value, struct{}{}); loaded {
		return
	}
	log.WithField("category", value).Warn("channel monitor: ignoring unknown error category")
}

// ValidateConfig reports why a management write should be rejected.
// Zero refresh interval is accepted and means the runtime default.
func ValidateConfig(cfg config.ChannelMonitorConfig) string {
	switch cfg.RefreshIntervalSeconds {
	case 0, 60, 300:
	default:
		return "refresh-interval-seconds must be 60 or 300"
	}
	unknown := unknownCategories(cfg.IgnoredErrorCategories)
	if len(unknown) == 0 {
		return ""
	}
	return "unknown ignored-error-categories: " + strings.Join(unknown, ", ")
}

func unknownCategories(values []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || knownCategory(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func ignoredSet(categories []string) map[string]struct{} {
	set := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		set[category] = struct{}{}
	}
	return set
}
