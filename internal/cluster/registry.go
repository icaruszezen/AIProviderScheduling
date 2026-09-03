package cluster

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	NodeStatusOnline = "online"
	NodeStatusStale  = "stale"
	NodeStatusError  = "error"
)

// Registration is sent by a slave when it joins a master.
type Registration struct {
	NodeID       string `json:"node_id"`
	AdvertiseURL string `json:"advertise_url"`
	Hostname     string `json:"hostname"`
	Version      string `json:"cpa_version"`
}

// Heartbeat is a periodic slave status report. It never includes credentials.
type Heartbeat struct {
	NodeID        string `json:"node_id"`
	AdvertiseURL  string `json:"advertise_url"`
	Hostname      string `json:"hostname"`
	Version       string `json:"cpa_version"`
	AppliedHash   string `json:"applied_hash"`
	AuthFileCount int    `json:"auth_file_count"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	// HeartbeatIntervalSeconds is how often this node intends to report. The
	// master judges staleness against it, because the two nodes can be
	// configured with different intervals.
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds,omitempty"`
}

// NodeStatus is the master-side view of a registered slave.
type NodeStatus struct {
	NodeID        string    `json:"node_id"`
	AdvertiseURL  string    `json:"advertise_url"`
	Hostname      string    `json:"hostname"`
	Version       string    `json:"cpa_version"`
	AppliedHash   string    `json:"applied_hash"`
	AuthFileCount int       `json:"auth_file_count"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	LastSeen      time.Time `json:"last_seen"`
	LastError     string    `json:"last_error,omitempty"`
	Status        string    `json:"status"`
}

type registeredNode struct {
	NodeID            string
	AdvertiseURL      string
	Hostname          string
	Version           string
	AppliedHash       string
	AuthFileCount     int
	UptimeSeconds     int64
	LastSeen          time.Time
	LastError         string
	HeartbeatInterval time.Duration
}

// Registry is an in-memory slave roster used by a master node.
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*registeredNode
}

func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]*registeredNode)}
}

func (r *Registry) UpsertRegistration(req Registration) {
	if r == nil {
		return
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node := r.nodes[nodeID]
	if node == nil {
		node = &registeredNode{NodeID: nodeID}
		r.nodes[nodeID] = node
	}
	if url := strings.TrimSpace(req.AdvertiseURL); url != "" {
		node.AdvertiseURL = strings.TrimRight(url, "/")
	}
	if req.Hostname != "" {
		node.Hostname = req.Hostname
	}
	if req.Version != "" {
		node.Version = req.Version
	}
	node.LastSeen = time.Now().UTC()
	node.LastError = ""
}

// UpsertHeartbeat records a slave report. targetHash is the master's current
// config hash; when the node reports that hash it is in sync, so any error left
// over from an earlier failed push is cleared. Without this a node that
// recovered by pulling on its own would stay red forever, because the next push
// is skipped precisely because the hashes already match.
func (r *Registry) UpsertHeartbeat(req Heartbeat, targetHash string) {
	if r == nil {
		return
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	node := r.nodes[nodeID]
	if node == nil {
		node = &registeredNode{NodeID: nodeID}
		r.nodes[nodeID] = node
	}
	if url := strings.TrimSpace(req.AdvertiseURL); url != "" {
		node.AdvertiseURL = strings.TrimRight(url, "/")
	}
	if req.Hostname != "" {
		node.Hostname = req.Hostname
	}
	if req.Version != "" {
		node.Version = req.Version
	}
	node.AppliedHash = req.AppliedHash
	node.AuthFileCount = req.AuthFileCount
	node.UptimeSeconds = req.UptimeSeconds
	if req.HeartbeatIntervalSeconds > 0 {
		node.HeartbeatInterval = time.Duration(req.HeartbeatIntervalSeconds) * time.Second
	}
	node.LastSeen = time.Now().UTC()
	targetHash = strings.TrimSpace(targetHash)
	if targetHash != "" && node.AppliedHash == targetHash {
		node.LastError = ""
	}
}

func (r *Registry) Remove(nodeID string) {
	if r == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	delete(r.nodes, nodeID)
	r.mu.Unlock()
}

func (r *Registry) SetError(nodeID, message string) {
	if r == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	if node := r.nodes[nodeID]; node != nil {
		node.LastError = strings.TrimSpace(message)
	}
	r.mu.Unlock()
}

func (r *Registry) SetAppliedHash(nodeID, hash string) {
	if r == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	r.mu.Lock()
	if node := r.nodes[nodeID]; node != nil {
		node.AppliedHash = hash
		node.LastError = ""
	}
	r.mu.Unlock()
}

// Snapshot lists the roster. fallbackStaleAfter applies only to nodes that have
// not reported their own heartbeat interval; every other node is judged against
// three of its own intervals, so a master and a slave configured with different
// intervals no longer mark each other stale.
func (r *Registry) Snapshot(fallbackStaleAfter time.Duration) []NodeStatus {
	if r == nil {
		return nil
	}
	if fallbackStaleAfter <= 0 {
		fallbackStaleAfter = 3 * time.Duration(DefaultHeartbeatSeconds()) * time.Second
	}
	now := time.Now().UTC()
	r.mu.RLock()
	out := make([]NodeStatus, 0, len(r.nodes))
	for _, node := range r.nodes {
		staleAfter := fallbackStaleAfter
		if node.HeartbeatInterval > 0 {
			staleAfter = 3 * node.HeartbeatInterval
		}
		status := NodeStatusOnline
		if node.LastSeen.IsZero() || now.Sub(node.LastSeen) > staleAfter {
			status = NodeStatusStale
		} else if strings.TrimSpace(node.LastError) != "" {
			status = NodeStatusError
		}
		out = append(out, NodeStatus{
			NodeID:        node.NodeID,
			AdvertiseURL:  node.AdvertiseURL,
			Hostname:      node.Hostname,
			Version:       node.Version,
			AppliedHash:   node.AppliedHash,
			AuthFileCount: node.AuthFileCount,
			UptimeSeconds: node.UptimeSeconds,
			LastSeen:      node.LastSeen,
			LastError:     node.LastError,
			Status:        status,
		})
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].NodeID < out[j].NodeID
	})
	return out
}

func DefaultHeartbeatSeconds() int {
	return 15
}
