package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

const pushConcurrency = 8

// NodeSelfStatus is the local node's cluster view for the management UI.
type NodeSelfStatus struct {
	Role                     string     `json:"role"`
	NodeID                   string     `json:"node_id"`
	TokenConfigured          bool       `json:"token_configured"`
	MasterURL                string     `json:"master_url"`
	AdvertiseURL             string     `json:"advertise_url"`
	AppliedHash              string     `json:"applied_hash"`
	LastSyncAt               *time.Time `json:"last_sync_at,omitempty"`
	LastError                string     `json:"last_error,omitempty"`
	SyncIntervalSeconds      int        `json:"sync_interval_seconds"`
	HeartbeatIntervalSeconds int        `json:"heartbeat_interval_seconds"`
}

// ApplyFunc persists a merged config and applies it to the running process.
type ApplyFunc func(ctx context.Context, cfg *config.Config) error

// ConfigFunc returns the current in-memory config.
type ConfigFunc func() *config.Config

// ConfigPathFunc returns the config.yaml path used for persistence.
type ConfigPathFunc func() string

// Service coordinates master push and slave pull/heartbeat.
type Service struct {
	cfgFn     ConfigFunc
	pathFn    ConfigPathFunc
	applyFn   ApplyFunc
	startedAt time.Time
	client    *Client
	registry  *Registry
	hostname  string

	mu             sync.Mutex
	rootCtx        context.Context
	rootCancel     context.CancelFunc
	loopCancel     context.CancelFunc
	currentRole    string
	currentSync    time.Duration
	currentBeat    time.Duration
	appliedHash    string
	appliedUpdated time.Time
	lastSyncAt     time.Time
	lastError      string
	pushRunning    bool
	pushRequeued   bool
}

// PushResult reports the outcome of one master push so callers can tell a
// partial failure from a clean sync.
type PushResult struct {
	NodeID       string `json:"node_id"`
	AdvertiseURL string `json:"advertise_url,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

// Push result statuses.
const (
	PushStatusPushed  = "pushed"
	PushStatusSkipped = "skipped"
	PushStatusFailed  = "failed"
)

func NewService(cfgFn ConfigFunc, pathFn ConfigPathFunc, applyFn ApplyFunc) *Service {
	hostname, _ := os.Hostname()
	return &Service{
		cfgFn:     cfgFn,
		pathFn:    pathFn,
		applyFn:   applyFn,
		startedAt: time.Now(),
		client:    NewClient(),
		registry:  NewRegistry(),
		hostname:  hostname,
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.rootCancel != nil {
		s.rootCancel()
	}
	rootCtx, cancel := context.WithCancel(ctx)
	s.rootCtx = rootCtx
	s.rootCancel = cancel
	s.mu.Unlock()
	s.ensureLoops()
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.unregisterBestEffort()
	s.mu.Lock()
	if s.loopCancel != nil {
		s.loopCancel()
		s.loopCancel = nil
	}
	if s.rootCancel != nil {
		s.rootCancel()
		s.rootCancel = nil
	}
	s.rootCtx = nil
	s.currentRole = ""
	s.currentSync = 0
	s.currentBeat = 0
	s.mu.Unlock()
}

func (s *Service) SyncRuntime() {
	if s == nil {
		return
	}
	s.ensureLoops()
}

// ensureLoops starts, stops, or restarts the slave loop to match the current
// config. It restarts on an interval change too, so editing the intervals takes
// effect without a process restart.
func (s *Service) ensureLoops() {
	cfg := s.config()
	role := config.ClusterRoleStandalone
	syncEvery := time.Duration(config.DefaultClusterSyncIntervalSeconds) * time.Second
	beatEvery := time.Duration(config.DefaultClusterHeartbeatIntervalSeconds) * time.Second
	if cfg != nil {
		role = cfg.Cluster.NormalizedRole()
		syncEvery = time.Duration(cfg.Cluster.SyncInterval()) * time.Second
		beatEvery = time.Duration(cfg.Cluster.HeartbeatInterval()) * time.Second
		if cfg.Home.Enabled {
			role = config.ClusterRoleStandalone
		}
	}
	s.mu.Lock()
	ctx := s.rootCtx
	unchanged := s.currentRole == role &&
		(role != config.ClusterRoleSlave ||
			(s.loopCancel != nil && s.currentSync == syncEvery && s.currentBeat == beatEvery))
	if unchanged {
		s.mu.Unlock()
		return
	}
	if s.loopCancel != nil {
		s.loopCancel()
		s.loopCancel = nil
	}
	s.currentRole = role
	s.currentSync = syncEvery
	s.currentBeat = beatEvery
	if role == config.ClusterRoleSlave && ctx != nil {
		loopCtx, cancel := context.WithCancel(ctx)
		s.loopCancel = cancel
		go s.runSlave(loopCtx, syncEvery, beatEvery)
	}
	s.mu.Unlock()
}

func (s *Service) config() *config.Config {
	if s == nil || s.cfgFn == nil {
		return nil
	}
	return s.cfgFn()
}

func (s *Service) configPath() string {
	if s == nil || s.pathFn == nil {
		return ""
	}
	return s.pathFn()
}

func (s *Service) Status() NodeSelfStatus {
	cfg := s.config()
	status := NodeSelfStatus{
		Role:                     config.ClusterRoleStandalone,
		SyncIntervalSeconds:      config.DefaultClusterSyncIntervalSeconds,
		HeartbeatIntervalSeconds: config.DefaultClusterHeartbeatIntervalSeconds,
	}
	if cfg != nil {
		status.Role = cfg.Cluster.NormalizedRole()
		status.NodeID = cfg.Cluster.NodeID
		status.MasterURL = cfg.Cluster.MasterURL
		status.AdvertiseURL = cfg.Cluster.AdvertiseURL
		status.SyncIntervalSeconds = cfg.Cluster.SyncInterval()
		status.HeartbeatIntervalSeconds = cfg.Cluster.HeartbeatInterval()
		status.TokenConfigured = config.ResolveClusterToken(cfg) != ""
		if cfg.Home.Enabled {
			status.Role = config.ClusterRoleStandalone
		}
	}
	s.mu.Lock()
	status.AppliedHash = s.appliedHash
	if !s.lastSyncAt.IsZero() {
		ts := s.lastSyncAt
		status.LastSyncAt = &ts
	}
	status.LastError = s.lastError
	s.mu.Unlock()
	return status
}

func (s *Service) Nodes() []NodeStatus {
	cfg := s.config()
	heartbeat := time.Duration(config.DefaultClusterHeartbeatIntervalSeconds) * time.Second
	if cfg != nil {
		heartbeat = time.Duration(cfg.Cluster.HeartbeatInterval()) * time.Second
	}
	return s.registry.Snapshot(3 * heartbeat)
}

func (s *Service) Register(req Registration) error {
	if err := s.requireMaster(); err != nil {
		return err
	}
	if strings.TrimSpace(req.NodeID) == "" {
		return fmt.Errorf("cluster: node_id is required")
	}
	s.registry.UpsertRegistration(req)
	return nil
}

func (s *Service) Heartbeat(req Heartbeat) error {
	if err := s.requireMaster(); err != nil {
		return err
	}
	if strings.TrimSpace(req.NodeID) == "" {
		return fmt.Errorf("cluster: node_id is required")
	}
	targetHash, err := HashOf(s.config())
	if err != nil {
		log.WithError(err).Debug("cluster: hash current config for heartbeat")
		targetHash = ""
	}
	s.registry.UpsertHeartbeat(req, targetHash)
	return nil
}

func (s *Service) Unregister(nodeID string) {
	s.registry.Remove(nodeID)
}

func (s *Service) RemoveNode(nodeID string) {
	s.registry.Remove(nodeID)
}

func (s *Service) Export() (*ExportPayload, error) {
	if err := s.requireMaster(); err != nil {
		return nil, err
	}
	return Export(s.config())
}

func (s *Service) Apply(ctx context.Context, payload *ExportPayload) error {
	if s == nil {
		return fmt.Errorf("cluster: service is nil")
	}
	cfg := s.config()
	if cfg == nil {
		return fmt.Errorf("cluster: config is unavailable")
	}
	if cfg.Home.Enabled {
		return fmt.Errorf("cluster: disabled in Home mode")
	}
	if !cfg.Cluster.IsSlave() {
		return fmt.Errorf("cluster: node is not a slave")
	}
	if payload == nil {
		return fmt.Errorf("cluster: apply payload is nil")
	}
	if payload.Protocol != 0 && payload.Protocol != protocolVersion {
		return fmt.Errorf("cluster: unsupported protocol %d", payload.Protocol)
	}
	if strings.TrimSpace(payload.ConfigYAML) == "" {
		return fmt.Errorf("cluster: config_yaml is empty")
	}
	if token := config.ResolveClusterToken(cfg); containsSecret(payload.ConfigYAML, token) {
		return fmt.Errorf("cluster: refusing payload that contains the cluster token; the master must strip cluster settings before exporting")
	}

	// A push and a scheduled pull can overlap, so an older snapshot may arrive
	// after a newer one. Applying it would roll the node back until the next sync.
	s.mu.Lock()
	appliedUpdated := s.appliedUpdated
	s.mu.Unlock()
	if !payload.UpdatedAt.IsZero() && !appliedUpdated.IsZero() && payload.UpdatedAt.Before(appliedUpdated) {
		return fmt.Errorf("cluster: payload from %s is older than the applied snapshot from %s",
			payload.UpdatedAt.UTC().Format(time.RFC3339), appliedUpdated.UTC().Format(time.RFC3339))
	}

	currentHash, err := HashOf(cfg)
	if err == nil && payload.Hash != "" && payload.Hash == currentHash {
		s.markApplied(payload)
		return nil
	}

	incoming, err := config.ParseConfigBytes([]byte(payload.ConfigYAML))
	if err != nil {
		return fmt.Errorf("cluster: parse exported config: %w", err)
	}
	merged := MergeLocal(cfg, incoming)
	path := s.configPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cluster: config path is empty")
	}

	if err = config.SaveConfigPreserveComments(path, merged); err != nil {
		s.setLastError(err.Error())
		return fmt.Errorf("cluster: save applied config: %w", err)
	}
	if s.applyFn != nil {
		if err = s.applyFn(ctx, merged); err != nil {
			s.setLastError(err.Error())
			return fmt.Errorf("cluster: apply config: %w", err)
		}
	}
	s.markApplied(payload)
	return nil
}

// markApplied records a successfully applied snapshot and clears the last error.
func (s *Service) markApplied(payload *ExportPayload) {
	s.mu.Lock()
	s.appliedHash = payload.Hash
	if payload.UpdatedAt.After(s.appliedUpdated) {
		s.appliedUpdated = payload.UpdatedAt
	}
	s.lastSyncAt = time.Now().UTC()
	s.lastError = ""
	s.mu.Unlock()
}

// EnqueuePush schedules a master push. Pushes are single-flight: while one is
// running, further requests only set a flag so the config that lands last is the
// config the slaves end up with, instead of racing concurrent pushes.
func (s *Service) EnqueuePush() {
	if s == nil {
		return
	}
	cfg := s.config()
	if cfg == nil || cfg.Home.Enabled || !cfg.Cluster.IsMaster() {
		return
	}
	s.mu.Lock()
	if s.pushRunning {
		s.pushRequeued = true
		s.mu.Unlock()
		return
	}
	s.pushRunning = true
	s.mu.Unlock()

	go func() {
		for {
			if _, err := s.SyncNow(context.Background()); err != nil {
				log.WithError(err).Warn("cluster: master push failed")
			}
			s.mu.Lock()
			if !s.pushRequeued {
				s.pushRunning = false
				s.mu.Unlock()
				return
			}
			s.pushRequeued = false
			s.mu.Unlock()
		}
	}()
}

// SyncNow pushes the current config to every registered slave and reports the
// per-node outcome. The returned error covers only failures that stopped the
// push from starting; individual node failures are carried in the results.
func (s *Service) SyncNow(ctx context.Context) ([]PushResult, error) {
	if err := s.requireMaster(); err != nil {
		return nil, err
	}
	payload, err := Export(s.config())
	if err != nil {
		return nil, err
	}
	token := config.ResolveClusterToken(s.config())
	if token == "" {
		return nil, errTokenNotUsable()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	nodes := s.Nodes()
	results := make([]PushResult, len(nodes))
	// Each goroutine owns one slot, so the slice needs no extra synchronisation.
	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(pushConcurrency)
	for i := range nodes {
		node := nodes[i]
		slot := &results[i]
		slot.NodeID = node.NodeID
		slot.AdvertiseURL = node.AdvertiseURL
		if strings.TrimSpace(node.AdvertiseURL) == "" {
			slot.Status = PushStatusFailed
			slot.Error = "advertise_url is empty"
			continue
		}
		if node.AppliedHash != "" && node.AppliedHash == payload.Hash {
			slot.Status = PushStatusSkipped
			continue
		}
		grp.Go(func() error {
			if errApply := s.client.Apply(grpCtx, node.AdvertiseURL, token, payload); errApply != nil {
				s.registry.SetError(node.NodeID, errApply.Error())
				log.WithError(errApply).WithField("node_id", node.NodeID).Warn("cluster: push to slave failed")
				slot.Status = PushStatusFailed
				slot.Error = errApply.Error()
				return nil
			}
			s.registry.SetAppliedHash(node.NodeID, payload.Hash)
			slot.Status = PushStatusPushed
			return nil
		})
	}
	if errWait := grp.Wait(); errWait != nil {
		return results, errWait
	}
	return results, nil
}

// errTokenNotUsable explains that the cluster secret is missing or too weak to use.
func errTokenNotUsable() error {
	return fmt.Errorf("cluster: token is not configured or shorter than %d characters", config.MinClusterTokenLength)
}

func (s *Service) requireMaster() error {
	cfg := s.config()
	if cfg == nil {
		return fmt.Errorf("cluster: config is unavailable")
	}
	if cfg.Home.Enabled {
		return fmt.Errorf("cluster: disabled in Home mode")
	}
	if !cfg.Cluster.IsMaster() {
		return fmt.Errorf("cluster: node is not a master")
	}
	return nil
}

func (s *Service) setLastError(message string) {
	s.mu.Lock()
	s.lastError = message
	s.mu.Unlock()
}

func (s *Service) runSlave(ctx context.Context, syncEvery, beatEvery time.Duration) {
	s.registerWithRetry(ctx)
	if ctx.Err() != nil {
		return
	}
	if err := s.pullOnce(ctx); err != nil && ctx.Err() == nil {
		log.WithError(err).Warn("cluster: initial pull from master failed")
		s.setLastError(err.Error())
	}
	if err := s.heartbeatOnce(ctx); err != nil && ctx.Err() == nil {
		log.WithError(err).Debug("cluster: initial heartbeat failed")
	}

	syncTicker := time.NewTicker(syncEvery)
	beatTicker := time.NewTicker(beatEvery)
	defer syncTicker.Stop()
	defer beatTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-syncTicker.C:
			if err := s.pullOnce(ctx); err != nil && ctx.Err() == nil {
				log.WithError(err).Warn("cluster: pull from master failed")
				s.setLastError(err.Error())
			}
		case <-beatTicker.C:
			if err := s.heartbeatOnce(ctx); err != nil && ctx.Err() == nil {
				log.WithError(err).Debug("cluster: heartbeat failed")
			}
		}
	}
}

func (s *Service) registerWithRetry(ctx context.Context) {
	delay := time.Second
	for {
		if err := s.registerOnce(ctx); err == nil {
			return
		} else if ctx.Err() != nil {
			return
		} else {
			log.WithError(err).Warn("cluster: register with master failed; retrying")
			s.setLastError(err.Error())
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

func (s *Service) registerOnce(ctx context.Context) error {
	cfg, token, masterURL, err := s.slavePeer()
	if err != nil {
		return err
	}
	return s.client.Register(ctx, masterURL, token, Registration{
		NodeID:       cfg.Cluster.NodeID,
		AdvertiseURL: cfg.Cluster.AdvertiseURL,
		Hostname:     s.hostname,
		Version:      buildinfo.Version,
	})
}

func (s *Service) heartbeatOnce(ctx context.Context) error {
	cfg, token, masterURL, err := s.slavePeer()
	if err != nil {
		return err
	}
	s.mu.Lock()
	applied := s.appliedHash
	s.mu.Unlock()
	return s.client.Heartbeat(ctx, masterURL, token, Heartbeat{
		NodeID:                   cfg.Cluster.NodeID,
		AdvertiseURL:             cfg.Cluster.AdvertiseURL,
		Hostname:                 s.hostname,
		Version:                  buildinfo.Version,
		AppliedHash:              applied,
		AuthFileCount:            countAuthFiles(cfg.AuthDir),
		UptimeSeconds:            int64(time.Since(s.startedAt).Seconds()),
		HeartbeatIntervalSeconds: cfg.Cluster.HeartbeatInterval(),
	})
}

func (s *Service) pullOnce(ctx context.Context) error {
	_, token, masterURL, err := s.slavePeer()
	if err != nil {
		return err
	}
	payload, err := s.client.Export(ctx, masterURL, token)
	if err != nil {
		return err
	}
	return s.Apply(ctx, payload)
}

func (s *Service) unregisterBestEffort() {
	cfg := s.config()
	if cfg == nil || !cfg.Cluster.IsSlave() {
		return
	}
	token := config.ResolveClusterToken(cfg)
	if token == "" || cfg.Cluster.MasterURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.client.Unregister(ctx, cfg.Cluster.MasterURL, token, cfg.Cluster.NodeID); err != nil {
		log.WithError(err).Debug("cluster: unregister from master failed")
	}
}

func (s *Service) slavePeer() (*config.Config, string, string, error) {
	cfg := s.config()
	if cfg == nil {
		return nil, "", "", fmt.Errorf("cluster: config is unavailable")
	}
	if cfg.Home.Enabled {
		return nil, "", "", fmt.Errorf("cluster: disabled in Home mode")
	}
	if !cfg.Cluster.IsSlave() {
		return nil, "", "", fmt.Errorf("cluster: node is not a slave")
	}
	token := config.ResolveClusterToken(cfg)
	if token == "" {
		return nil, "", "", errTokenNotUsable()
	}
	if cfg.Cluster.MasterURL == "" {
		return nil, "", "", fmt.Errorf("cluster: master-url is not configured")
	}
	if cfg.Cluster.NodeID == "" {
		return nil, "", "", fmt.Errorf("cluster: node-id is empty")
	}
	return cfg, token, cfg.Cluster.MasterURL, nil
}

func countAuthFiles(authDir string) int {
	resolved, err := util.ResolveAuthDir(authDir)
	if err != nil || resolved == "" {
		return 0
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			count++
		}
	}
	return count
}
