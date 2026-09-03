package config

import (
	"os"
	"strings"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

const (
	// ClusterRoleStandalone is the default single-node role.
	ClusterRoleStandalone = "standalone"
	// ClusterRoleMaster publishes config to registered slave nodes.
	ClusterRoleMaster = "master"
	// ClusterRoleSlave receives config from a master node.
	ClusterRoleSlave = "slave"

	// DefaultClusterSyncIntervalSeconds is the slave pull interval.
	DefaultClusterSyncIntervalSeconds = 30
	// DefaultClusterHeartbeatIntervalSeconds is the slave heartbeat interval.
	DefaultClusterHeartbeatIntervalSeconds = 15

	// MinClusterTokenLength is the shortest accepted cluster protocol secret.
	// The protocol endpoints are reachable without allow-remote, so a weak token
	// is treated as "not configured" rather than silently guarding the config export.
	MinClusterTokenLength = 32

	// MinClusterIntervalSeconds bounds how aggressively a slave may poll its master.
	MinClusterIntervalSeconds = 5

	clusterTokenEnv = "CLUSTER_TOKEN"
)

// ClusterConfig holds per-node master/slave sync settings.
// These fields are never overwritten by cluster apply.
type ClusterConfig struct {
	// Role is standalone, master, or slave. Empty is treated as standalone.
	Role string `yaml:"role" json:"role"`
	// Token is the shared cluster protocol secret. It is never returned by GET /config.
	Token string `yaml:"token" json:"-"`
	// NodeID uniquely identifies this node. Generated locally when empty for master/slave.
	NodeID string `yaml:"node-id,omitempty" json:"node-id,omitempty"`
	// MasterURL is the slave-only root URL of the master, e.g. http://192.168.1.10:8317.
	MasterURL string `yaml:"master-url,omitempty" json:"master-url,omitempty"`
	// AdvertiseURL is the slave-only URL the master uses to push config.
	AdvertiseURL string `yaml:"advertise-url,omitempty" json:"advertise-url,omitempty"`
	// SyncIntervalSeconds is the slave pull interval. Default 30.
	SyncIntervalSeconds int `yaml:"sync-interval-seconds,omitempty" json:"sync-interval-seconds,omitempty"`
	// HeartbeatIntervalSeconds is the slave heartbeat interval. Default 15.
	HeartbeatIntervalSeconds int `yaml:"heartbeat-interval-seconds,omitempty" json:"heartbeat-interval-seconds,omitempty"`
}

// ClusterRoleIsValid reports whether role names a cluster role. Empty means
// standalone. Callers that accept operator input should reject anything else
// instead of letting NormalizeRole silently downgrade a typo to standalone.
func ClusterRoleIsValid(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", ClusterRoleStandalone, ClusterRoleMaster, ClusterRoleSlave:
		return true
	default:
		return false
	}
}

// NormalizeRole maps aliases and empty values to a canonical cluster role.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case ClusterRoleMaster:
		return ClusterRoleMaster
	case ClusterRoleSlave:
		return ClusterRoleSlave
	default:
		return ClusterRoleStandalone
	}
}

// NormalizedRole returns the canonical role for this cluster config.
func (c ClusterConfig) NormalizedRole() string {
	return NormalizeRole(c.Role)
}

// IsSlave reports whether the node is a config replica.
func (c ClusterConfig) IsSlave() bool {
	return c.NormalizedRole() == ClusterRoleSlave
}

// IsMaster reports whether the node publishes config to slaves.
func (c ClusterConfig) IsMaster() bool {
	return c.NormalizedRole() == ClusterRoleMaster
}

// SyncInterval returns the slave pull interval, falling back to the default.
func (c ClusterConfig) SyncInterval() int {
	if c.SyncIntervalSeconds <= 0 {
		return DefaultClusterSyncIntervalSeconds
	}
	if c.SyncIntervalSeconds < MinClusterIntervalSeconds {
		return MinClusterIntervalSeconds
	}
	return c.SyncIntervalSeconds
}

// HeartbeatInterval returns the slave heartbeat interval, falling back to the default.
func (c ClusterConfig) HeartbeatInterval() int {
	if c.HeartbeatIntervalSeconds <= 0 {
		return DefaultClusterHeartbeatIntervalSeconds
	}
	if c.HeartbeatIntervalSeconds < MinClusterIntervalSeconds {
		return MinClusterIntervalSeconds
	}
	return c.HeartbeatIntervalSeconds
}

// ClusterTokenIsUsable reports whether token is long enough to guard the cluster
// protocol endpoints.
func ClusterTokenIsUsable(token string) bool {
	return len(strings.TrimSpace(token)) >= MinClusterTokenLength
}

// ResolveClusterToken returns CLUSTER_TOKEN when set, otherwise the config token.
// A token shorter than MinClusterTokenLength is reported as unset so that a weak
// secret never enables the cluster protocol.
func ResolveClusterToken(cfg *Config) string {
	if env := strings.TrimSpace(os.Getenv(clusterTokenEnv)); env != "" {
		if !ClusterTokenIsUsable(env) {
			return ""
		}
		return env
	}
	if cfg == nil {
		return ""
	}
	token := strings.TrimSpace(cfg.Cluster.Token)
	if !ClusterTokenIsUsable(token) {
		return ""
	}
	return token
}

// NormalizeCluster trims cluster fields and assigns defaults.
// It returns true when a new node-id was generated and should be persisted locally.
func (cfg *Config) NormalizeCluster() bool {
	if cfg == nil {
		return false
	}
	cluster := cfg.Cluster
	cluster.Role = NormalizeRole(cluster.Role)
	cluster.Token = strings.TrimSpace(cluster.Token)
	cluster.NodeID = strings.TrimSpace(cluster.NodeID)
	cluster.MasterURL = strings.TrimRight(strings.TrimSpace(cluster.MasterURL), "/")
	cluster.AdvertiseURL = strings.TrimRight(strings.TrimSpace(cluster.AdvertiseURL), "/")
	if cluster.SyncIntervalSeconds <= 0 {
		cluster.SyncIntervalSeconds = DefaultClusterSyncIntervalSeconds
	} else if cluster.SyncIntervalSeconds < MinClusterIntervalSeconds {
		cluster.SyncIntervalSeconds = MinClusterIntervalSeconds
	}
	if cluster.HeartbeatIntervalSeconds <= 0 {
		cluster.HeartbeatIntervalSeconds = DefaultClusterHeartbeatIntervalSeconds
	} else if cluster.HeartbeatIntervalSeconds < MinClusterIntervalSeconds {
		cluster.HeartbeatIntervalSeconds = MinClusterIntervalSeconds
	}
	if cluster.Token != "" && !ClusterTokenIsUsable(cluster.Token) {
		log.Warnf("cluster: token is shorter than %d characters; cluster sync stays disabled until a stronger secret is configured", MinClusterTokenLength)
	}
	generated := false
	if (cluster.Role == ClusterRoleMaster || cluster.Role == ClusterRoleSlave) && cluster.NodeID == "" {
		cluster.NodeID = uuid.NewString()
		generated = true
	}
	cfg.Cluster = cluster
	return generated
}
