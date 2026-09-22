package management

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/channelmonitor"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// SetChannelMonitor installs the passive traffic aggregator used by management reads.
func (h *Handler) SetChannelMonitor(monitor *channelmonitor.Service) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.channelMonitor = monitor
	h.mu.Unlock()
}

func (h *Handler) channelMonitorService() *channelmonitor.Service {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	monitor := h.channelMonitor
	h.mu.Unlock()
	return monitor
}

// GetChannelMonitorConfig returns the effective channel-monitor settings.
func (h *Handler) GetChannelMonitorConfig(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config unavailable"})
		return
	}
	c.JSON(http.StatusOK, channelmonitor.Effective(h.cfg.ChannelMonitor))
}

// PutChannelMonitorConfig replaces the channel-monitor block and reloads it.
// PATCH merges only the JSON fields present in the body.
func (h *Handler) PutChannelMonitorConfig(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "config unavailable"})
		return
	}
	if c.Request != nil && c.Request.Method == http.MethodPatch {
		h.patchChannelMonitorConfig(c)
		return
	}
	var body config.ChannelMonitorConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if message := channelmonitor.ValidateConfig(body); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}
	h.saveChannelMonitor(c, body)
}

func (h *Handler) patchChannelMonitorConfig(c *gin.Context) {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	next := h.cfg.ChannelMonitor
	h.mu.Unlock()
	if err := applyChannelMonitorPatch(&next, raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if message := channelmonitor.ValidateConfig(next); message != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return
	}
	h.saveChannelMonitor(c, next)
}

func (h *Handler) saveChannelMonitor(c *gin.Context, body config.ChannelMonitorConfig) {
	body = body.Normalized()
	h.mu.Lock()
	defer h.mu.Unlock()
	previous := h.cfg.ChannelMonitor
	h.cfg.ChannelMonitor = body
	if !h.persistLocked(c) {
		h.cfg.ChannelMonitor = previous
	}
}

func applyChannelMonitorPatch(cfg *config.ChannelMonitorConfig, raw map[string]json.RawMessage) error {
	if cfg == nil {
		return nil
	}
	decoders := []struct {
		key    string
		target any
	}{
		{key: "enabled", target: &cfg.Enabled},
		{key: "refresh-interval-seconds", target: &cfg.RefreshIntervalSeconds},
		{key: "database-path", target: &cfg.DatabasePath},
		{key: "auth-indexes", target: &cfg.AuthIndexes},
		{key: "providers", target: &cfg.Providers},
		{key: "models", target: &cfg.Models},
		{key: "ignored-error-categories", target: &cfg.IgnoredErrorCategories},
		{key: "health-thresholds", target: &cfg.HealthThresholds},
	}
	for _, field := range decoders {
		payload, ok := raw[field.key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(payload, field.target); err != nil {
			return err
		}
	}
	return nil
}

// GetChannelMonitorSummaries returns one thumbnail per credential with traffic.
func (h *Handler) GetChannelMonitorSummaries(c *gin.Context) {
	monitor := h.channelMonitorService()
	if monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "channel monitor unavailable"})
		return
	}
	summary, err := monitor.Summaries(channelMonitorQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channel monitor query failed"})
		return
	}
	c.JSON(http.StatusOK, enrichedSummary{SummaryResponse: summary, Items: h.enrichSummaryItems(summary.Items)})
}

// GetChannelMonitorSnapshot returns the filtered aggregate and trend.
func (h *Handler) GetChannelMonitorSnapshot(c *gin.Context) {
	monitor := h.channelMonitorService()
	if monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "channel monitor unavailable"})
		return
	}
	snapshot, err := monitor.Snapshot(channelMonitorQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channel monitor query failed"})
		return
	}
	c.JSON(http.StatusOK, snapshot)
}

// GetChannelMonitorModels returns per-model rows for the current filter.
func (h *Handler) GetChannelMonitorModels(c *gin.Context) {
	monitor := h.channelMonitorService()
	if monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "channel monitor unavailable"})
		return
	}
	models, err := monitor.Models(channelMonitorQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channel monitor query failed"})
		return
	}
	c.JSON(http.StatusOK, models)
}

// GetChannelMonitorErrors returns taxonomy counts for the current filter.
func (h *Handler) GetChannelMonitorErrors(c *gin.Context) {
	monitor := h.channelMonitorService()
	if monitor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "channel monitor unavailable"})
		return
	}
	errors, err := monitor.Errors(channelMonitorQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channel monitor query failed"})
		return
	}
	c.JSON(http.StatusOK, errors)
}

type enrichedSummary struct {
	channelmonitor.SummaryResponse
	Items []enrichedSummaryItem `json:"items"`
}

type enrichedSummaryItem struct {
	channelmonitor.SummaryItem
	Label        string `json:"label,omitempty"`
	APIKeyMasked string `json:"api_key_masked,omitempty"`
	Present      bool   `json:"present"`
}

func (h *Handler) enrichSummaryItems(items []channelmonitor.SummaryItem) []enrichedSummaryItem {
	auths := h.authsByIndex()
	out := make([]enrichedSummaryItem, 0, len(items))
	for _, item := range items {
		enriched := enrichedSummaryItem{SummaryItem: item}
		if auth := auths[item.AuthIndex]; auth != nil {
			enriched.Present = true
			enriched.Label = strings.TrimSpace(auth.Label)
			if enriched.Label == "" {
				enriched.Label = strings.TrimSpace(auth.Provider)
			}
			_, secret := auth.AccountInfo()
			enriched.APIKeyMasked = maskCredential(secret)
		}
		out = append(out, enriched)
	}
	return out
}

func (h *Handler) authsByIndex() map[string]*coreauth.Auth {
	out := map[string]*coreauth.Auth{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return out
	}
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		index := strings.TrimSpace(auth.Index)
		if index == "" {
			index = auth.EnsureIndex()
		}
		if index == "" {
			continue
		}
		out[index] = auth
	}
	return out
}

func channelMonitorQuery(c *gin.Context) channelmonitor.Query {
	return channelmonitor.Query{
		Range:       c.Query("range"),
		Providers:   splitMonitorQuery(c, "provider"),
		AuthIndexes: splitMonitorQuery(c, "auth_index"),
		Models:      splitMonitorQuery(c, "model"),
	}
}

func splitMonitorQuery(c *gin.Context, key string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, part := range c.QueryArray(key) {
		for _, piece := range strings.Split(part, ",") {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			if _, ok := seen[piece]; ok {
				continue
			}
			seen[piece] = struct{}{}
			out = append(out, piece)
		}
	}
	return out
}

func maskCredential(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "***"
	}
	return value[:4] + "***"
}
