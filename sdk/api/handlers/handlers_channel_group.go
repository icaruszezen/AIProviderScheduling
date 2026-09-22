package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// UsesChannelGroup reports whether the caller authenticated with a group API key.
func (h *BaseAPIHandler) UsesChannelGroup(c *gin.Context) bool {
	_, _, ok := channelGroupFromGin(c)
	return ok
}

// ChannelGroupModels returns models registered by the caller's group.
// The boolean is false when the caller is not using a group API key.
func (h *BaseAPIHandler) ChannelGroupModels(c *gin.Context, handlerType string) ([]map[string]any, bool) {
	ids, ok := h.channelGroupClientIDs(c)
	if !ok {
		return nil, false
	}
	return registry.GetGlobalRegistry().GetAvailableModelsForClients(handlerType, ids), true
}

// ChannelGroupModelInfos returns model metadata registered by the caller's group.
// The boolean is false when the caller is not using a group API key.
func (h *BaseAPIHandler) ChannelGroupModelInfos(c *gin.Context) ([]*registry.ModelInfo, bool) {
	ids, ok := h.channelGroupClientIDs(c)
	if !ok {
		return nil, false
	}
	return registry.GetGlobalRegistry().GetAvailableModelInfosForClients(ids), true
}

func (h *BaseAPIHandler) channelGroupClientIDs(c *gin.Context) ([]string, bool) {
	_, _, ok := channelGroupFromGin(c)
	if !ok {
		return nil, false
	}
	if h == nil || h.AuthManager == nil {
		return nil, true
	}
	panel, group, _ := channelGroupFromGin(c)
	return h.AuthManager.AuthIDsForChannelGroup(panel, group), true
}

func channelGroupFromGin(c *gin.Context) (string, string, bool) {
	if c == nil {
		return "", "", false
	}
	raw, exists := c.Get("accessMetadata")
	if !exists || raw == nil {
		return "", "", false
	}
	metadata, ok := raw.(map[string]string)
	if !ok {
		return "", "", false
	}
	panel := strings.TrimSpace(metadata[coreauth.ChannelGroupProviderMetadataKey])
	group := strings.TrimSpace(metadata[coreauth.ChannelGroupMetadataKey])
	if panel == "" || group == "" {
		return "", "", false
	}
	return panel, group, true
}
