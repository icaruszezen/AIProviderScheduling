package management

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/cluster"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// ClusterController is the runtime used by management cluster endpoints.
type ClusterController interface {
	Status() cluster.NodeSelfStatus
	Nodes() []cluster.NodeStatus
	Register(cluster.Registration) error
	Heartbeat(cluster.Heartbeat) error
	Unregister(nodeID string)
	RemoveNode(nodeID string)
	Export() (*cluster.ExportPayload, error)
	Apply(ctx context.Context, payload *cluster.ExportPayload) error
	SyncNow(ctx context.Context) ([]cluster.PushResult, error)
}

// SetClusterController attaches the cluster runtime to the management handler.
func (h *Handler) SetClusterController(ctrl ClusterController) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cluster = ctrl
	h.mu.Unlock()
}

func (h *Handler) clusterController() ClusterController {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cluster
}

// isSlaveConfigReadonly reports whether synced-config writes must be rejected.
func (h *Handler) isSlaveConfigReadonly() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.slaveReadonlyLocked()
}

// slaveReadonlyLocked reports slave write protection. Caller must hold h.mu.
func (h *Handler) slaveReadonlyLocked() bool {
	if h == nil {
		return false
	}
	cfg := h.cfg
	if cfg == nil || cfg.Home.Enabled || !cfg.Cluster.IsSlave() {
		return false
	}
	return true
}

// SlaveConfigWriteGuard rejects synced-config writes on slave nodes.
func (h *Handler) SlaveConfigWriteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if !h.isSlaveConfigReadonly() {
			c.Next()
			return
		}
		if slaveWriteAllowed(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":   "slave_node_readonly",
			"message": "slave node: config is synced from master",
		})
	}
}

func slaveWriteAllowed(method, path string) bool {
	path = strings.TrimSpace(path)
	switch {
	// Installing a plugin is already allowed because the binary lives in the
	// node-local plugins.dir, which is never synced. Uninstalling it is the same
	// kind of local operation, so allow it too instead of letting the UI offer a
	// delete button that always fails.
	case method == http.MethodDelete && isPluginInstancePath(path):
		return true
	case path == "/v0/management/cluster", strings.HasPrefix(path, "/v0/management/cluster/"):
		return true
	case strings.HasPrefix(path, "/v0/management/auth-files"):
		return true
	case path == "/v0/management/oauth" || path == "/v0/management/oauth-callback" || path == "/v0/management/oauth-session":
		return true
	case strings.HasSuffix(path, "-auth-url"):
		return true
	case path == "/v0/management/get-auth-status":
		return true
	case path == "/v0/management/logs" || strings.HasPrefix(path, "/v0/management/request-error-logs"):
		return true
	case path == "/v0/management/reset-quota":
		return true
	case path == "/v0/management/vertex/import":
		return true
	case path == "/v0/management/api-call":
		return true
	case strings.HasPrefix(path, "/v0/management/plugin-store"):
		return true
	default:
		return false
	}
}

// isPluginInstancePath matches /v0/management/plugins/<id> and nothing deeper,
// so the enabled and config sub-resources stay under the readonly guard.
func isPluginInstancePath(path string) bool {
	const prefix = "/v0/management/plugins/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	return id != "" && !strings.Contains(id, "/")
}

// ClusterTokenMiddleware authenticates cluster protocol requests.
// It does not require allow-remote, so it shares the management key ban tracker
// to keep the token from being brute forced.
func (h *Handler) ClusterTokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if remaining := h.banRemaining(clientIP, time.Now()); remaining > 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("IP banned due to too many failed attempts. Try again in %s", remaining),
			})
			return
		}
		provided := clusterTokenFromRequest(c)
		if !h.authenticateClusterToken(provided) {
			h.recordAuthFailure(clientIP)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid cluster token"})
			return
		}
		h.resetAuthFailures(clientIP)
		c.Next()
	}
}

func clusterTokenFromRequest(c *gin.Context) string {
	if ah := c.GetHeader("Authorization"); ah != "" {
		parts := strings.SplitN(ah, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return parts[1]
		}
		return ah
	}
	if token := c.GetHeader("X-Cluster-Token"); token != "" {
		return token
	}
	return c.GetHeader("X-Management-Key")
}

func (h *Handler) authenticateClusterToken(provided string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	expected := config.ResolveClusterToken(cfg)
	if expected == "" || provided == "" {
		return false
	}
	if len(expected) != len(provided) {
		dummy := make([]byte, len(expected))
		_ = subtle.ConstantTimeCompare([]byte(expected), dummy)
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (h *Handler) GetCluster(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl != nil {
		c.JSON(http.StatusOK, ctrl.Status())
		return
	}
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	status := cluster.NodeSelfStatus{Role: config.ClusterRoleStandalone}
	if cfg != nil {
		status.Role = cfg.Cluster.NormalizedRole()
		status.NodeID = cfg.Cluster.NodeID
		status.MasterURL = cfg.Cluster.MasterURL
		status.AdvertiseURL = cfg.Cluster.AdvertiseURL
		status.TokenConfigured = config.ResolveClusterToken(cfg) != ""
		status.SyncIntervalSeconds = cfg.Cluster.SyncInterval()
		status.HeartbeatIntervalSeconds = cfg.Cluster.HeartbeatInterval()
		if cfg.Home.Enabled {
			status.Role = config.ClusterRoleStandalone
		}
	}
	c.JSON(http.StatusOK, status)
}

func (h *Handler) GetClusterNodes(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusOK, gin.H{"nodes": []cluster.NodeStatus{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": ctrl.Nodes()})
}

func (h *Handler) PostClusterSync(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	results, err := ctrl.SyncNow(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if results == nil {
		results = []cluster.PushResult{}
	}
	failed := 0
	for i := range results {
		if results[i].Status == cluster.PushStatusFailed {
			failed++
		}
	}
	status := "ok"
	code := http.StatusOK
	if failed > 0 {
		// The caller asked for a push; some nodes did not get it. Reporting 200
		// here would make a broken cluster look healthy in the UI.
		status = "partial"
		code = http.StatusMultiStatus
	}
	c.JSON(code, gin.H{"status": status, "failed": failed, "nodes": results})
}

func (h *Handler) DeleteClusterNode(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	nodeID := strings.TrimSpace(c.Param("id"))
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_id is required"})
		return
	}
	ctrl.RemoveNode(nodeID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type clusterPatchBody struct {
	Role                     *string `json:"role"`
	Token                    *string `json:"token"`
	MasterURL                *string `json:"master_url"`
	AdvertiseURL             *string `json:"advertise_url"`
	SyncIntervalSeconds      *int    `json:"sync_interval_seconds"`
	HeartbeatIntervalSeconds *int    `json:"heartbeat_interval_seconds"`
}

func (h *Handler) PatchCluster(c *gin.Context) {
	var body clusterPatchBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": err.Error()})
		return
	}
	if body.Role != nil && !config.ClusterRoleIsValid(*body.Role) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_role",
			"message": fmt.Sprintf("role must be one of %s, %s, %s", config.ClusterRoleStandalone, config.ClusterRoleMaster, config.ClusterRoleSlave),
		})
		return
	}
	if err := validateClusterInterval("sync_interval_seconds", body.SyncIntervalSeconds); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_interval", "message": err.Error()})
		return
	}
	if err := validateClusterInterval("heartbeat_interval_seconds", body.HeartbeatIntervalSeconds); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_interval", "message": err.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config is unavailable"})
		return
	}
	next := h.cfg.Cluster
	if body.Role != nil {
		next.Role = config.NormalizeRole(*body.Role)
	}
	if body.Token != nil {
		next.Token = strings.TrimSpace(*body.Token)
	}
	if body.MasterURL != nil {
		next.MasterURL = strings.TrimRight(strings.TrimSpace(*body.MasterURL), "/")
	}
	if body.AdvertiseURL != nil {
		next.AdvertiseURL = strings.TrimRight(strings.TrimSpace(*body.AdvertiseURL), "/")
	}
	if body.SyncIntervalSeconds != nil {
		next.SyncIntervalSeconds = *body.SyncIntervalSeconds
	}
	if body.HeartbeatIntervalSeconds != nil {
		next.HeartbeatIntervalSeconds = *body.HeartbeatIntervalSeconds
	}
	// A slave without a master silently never syncs, so reject it here rather
	// than leaving the node in a role it cannot fulfil.
	if next.IsSlave() && next.MasterURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "master_url_required",
			"message": "master_url is required when role is slave",
		})
		return
	}
	h.cfg.Cluster = next
	h.cfg.NormalizeCluster()
	if !h.persistClusterLocalLocked(c) {
		return
	}
}

// validateClusterInterval rejects intervals the runtime would silently clamp.
func validateClusterInterval(field string, seconds *int) error {
	if seconds == nil || *seconds == 0 {
		return nil
	}
	if *seconds < config.MinClusterIntervalSeconds {
		return fmt.Errorf("%s must be at least %d seconds, or 0 to use the default", field, config.MinClusterIntervalSeconds)
	}
	return nil
}

func (h *Handler) PostClusterRegister(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	var req cluster.Registration
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ctrl.Register(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) PostClusterHeartbeat(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	var req cluster.Heartbeat
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ctrl.Heartbeat(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) PostClusterUnregister(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	var body struct {
		NodeID string `json:"node_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctrl.Unregister(body.NodeID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) GetClusterExport(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	payload, err := ctrl.Export()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (h *Handler) PutClusterApply(c *gin.Context) {
	ctrl := h.clusterController()
	if ctrl == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster runtime is not available"})
		return
	}
	var payload cluster.ExportPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ctrl.Apply(c.Request.Context(), &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "hash": payload.Hash})
}
