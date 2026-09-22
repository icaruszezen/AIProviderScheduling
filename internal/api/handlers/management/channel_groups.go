package management

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func (h *Handler) GetChannelGroups(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	h.mu.Lock()
	groups := h.cfg.ChannelGroups
	h.mu.Unlock()
	if groups == nil {
		groups = map[string][]config.ChannelGroup{}
	}
	c.JSON(http.StatusOK, gin.H{"channel-groups": groups})
}

func (h *Handler) PutChannelGroups(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	data, errRead := c.GetRawData()
	if errRead != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	groups, errDecode := decodeChannelGroups(data)
	if errDecode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	candidate := &config.Config{
		SDKConfig:     h.cfg.SDKConfig,
		ChannelGroups: groups,
	}
	if errNormalize := candidate.NormalizeChannelGroups(); errNormalize != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errNormalize.Error()})
		return
	}
	h.cfg.ChannelGroups = candidate.ChannelGroups
	h.persistLocked(c)
}

func decodeChannelGroups(data []byte) (map[string][]config.ChannelGroup, error) {
	var wrapped struct {
		Groups map[string][]config.ChannelGroup `json:"channel-groups"`
	}
	if errUnmarshal := json.Unmarshal(data, &wrapped); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if wrapped.Groups != nil {
		return wrapped.Groups, nil
	}
	var groups map[string][]config.ChannelGroup
	if errDirect := json.Unmarshal(data, &groups); errDirect != nil {
		return nil, errDirect
	}
	return groups, nil
}
