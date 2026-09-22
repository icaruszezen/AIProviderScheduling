package management

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

func (h *Handler) GetChannelGroups(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "handler not initialized"})
		return
	}
	h.mu.Lock()
	groups := h.cfg.ChannelGroups
	h.mu.Unlock()
	if groups == nil {
		groups = map[string][]string{}
	}
	c.JSON(200, gin.H{"channel-groups": groups})
}

func (h *Handler) PutChannelGroups(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(500, gin.H{"error": "handler not initialized"})
		return
	}
	data, errRead := c.GetRawData()
	if errRead != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var wrapped struct {
		Groups map[string][]string `json:"channel-groups"`
	}
	if errUnmarshal := json.Unmarshal(data, &wrapped); errUnmarshal != nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}
	groups := wrapped.Groups
	if groups == nil {
		if errDirect := json.Unmarshal(data, &groups); errDirect != nil {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.ChannelGroups = groups
	h.cfg.NormalizeChannelGroups()
	h.persistLocked(c)
}
