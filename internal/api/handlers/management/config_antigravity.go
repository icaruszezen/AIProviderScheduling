package management

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func (h *Handler) GetAntigravityKeys(c *gin.Context) {
	c.JSON(200, gin.H{"antigravity-api-key": h.antigravityKeysWithAuthIndex()})
}

func (h *Handler) PutAntigravityKeys(c *gin.Context) {
	data, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}
	var arr []config.AntigravityKey
	if err = json.Unmarshal(data, &arr); err != nil {
		var obj struct {
			Items []config.AntigravityKey `json:"items"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil || len(obj.Items) == 0 {
			c.JSON(400, gin.H{"error": "invalid body"})
			return
		}
		arr = obj.Items
	}
	for index := range arr {
		if rejectInvalidCredentialWeight(c, fmt.Sprintf("antigravity-api-key[%d].weight", index), arr[index].Weight) {
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.AntigravityKey = append([]config.AntigravityKey(nil), arr...)
	h.cfg.SanitizeAntigravityKeys()
	h.persistLocked(c)
}

func (h *Handler) PatchAntigravityKey(c *gin.Context) {
	type antigravityKeyPatch struct {
		APIKey                   *string            `json:"api-key"`
		ProjectID                *string            `json:"project-id"`
		Weight                   json.RawMessage    `json:"weight"`
		Prefix                   *string            `json:"prefix"`
		BaseURL                  *string            `json:"base-url"`
		ProxyURL                 *string            `json:"proxy-url"`
		Headers                  *map[string]string `json:"headers"`
		ExcludedModels           *[]string          `json:"excluded-models"`
		DisableCooling           json.RawMessage    `json:"disable-cooling"`
		RequestRetry             *int               `json:"request-retry"`
		ProviderRetryCount       json.RawMessage    `json:"provider-retry-count"`
		ProviderRetryStatusCodes json.RawMessage    `json:"provider-retry-status-codes"`
		HideNoAvailableChannel   *bool              `json:"hide-no-available-channel"`
	}
	var body struct {
		Index *int                 `json:"index"`
		Match *string              `json:"match"`
		Value *antigravityKeyPatch `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	targetIndex := -1
	if body.Index != nil && *body.Index >= 0 && *body.Index < len(h.cfg.AntigravityKey) {
		targetIndex = *body.Index
	}
	if targetIndex == -1 && body.Match != nil {
		match := strings.TrimSpace(*body.Match)
		if match != "" {
			matches := make([]int, 0, 1)
			for i := range h.cfg.AntigravityKey {
				if strings.TrimSpace(h.cfg.AntigravityKey[i].APIKey) == match {
					matches = append(matches, i)
				}
			}
			if len(matches) > 1 {
				c.JSON(400, gin.H{"error": "multiple items match; index is required"})
				return
			}
			if len(matches) == 1 {
				targetIndex = matches[0]
			}
		}
	}
	if targetIndex == -1 {
		c.JSON(404, gin.H{"error": "item not found"})
		return
	}

	entry := h.cfg.AntigravityKey[targetIndex]
	if body.Value.APIKey != nil {
		entry.APIKey = strings.TrimSpace(*body.Value.APIKey)
	}
	if body.Value.ProjectID != nil {
		entry.ProjectID = strings.TrimSpace(*body.Value.ProjectID)
	}
	if len(body.Value.Weight) > 0 {
		weight, errWeight := parseCredentialWeightPatch(body.Value.Weight)
		if errWeight != nil {
			c.JSON(400, gin.H{"error": errWeight.Error()})
			return
		}
		entry.Weight = weight
	}
	if body.Value.Prefix != nil {
		entry.Prefix = strings.TrimSpace(*body.Value.Prefix)
	}
	if body.Value.BaseURL != nil {
		entry.BaseURL = strings.TrimSpace(*body.Value.BaseURL)
	}
	if body.Value.ProxyURL != nil {
		entry.ProxyURL = strings.TrimSpace(*body.Value.ProxyURL)
	}
	if body.Value.Headers != nil {
		entry.Headers = config.NormalizeHeaders(*body.Value.Headers)
	}
	if body.Value.ExcludedModels != nil {
		entry.ExcludedModels = config.NormalizeExcludedModels(*body.Value.ExcludedModels)
	}
	if !applyDisableCoolingPatch(c, body.Value.DisableCooling, &entry.DisableCooling) {
		return
	}
	if body.Value.RequestRetry != nil {
		entry.RequestRetry = body.Value.RequestRetry
	}
	if !applyProviderRetryPatch(c, body.Value.ProviderRetryCount, body.Value.ProviderRetryStatusCodes, &entry.ProviderRetryCount, &entry.ProviderRetryStatusCodes) {
		return
	}
	if body.Value.HideNoAvailableChannel != nil {
		entry.HideNoAvailableChannel = *body.Value.HideNoAvailableChannel
	}
	if entry.APIKey == "" && entry.BaseURL == "" {
		h.cfg.AntigravityKey = append(h.cfg.AntigravityKey[:targetIndex], h.cfg.AntigravityKey[targetIndex+1:]...)
		h.cfg.SanitizeAntigravityKeys()
		h.persistLocked(c)
		return
	}
	h.cfg.AntigravityKey[targetIndex] = entry
	h.cfg.SanitizeAntigravityKeys()
	h.persistLocked(c)
}

func (h *Handler) DeleteAntigravityKey(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if val := strings.TrimSpace(c.Query("api-key")); val != "" {
		matchIndex := -1
		matchCount := 0
		for i := range h.cfg.AntigravityKey {
			if strings.TrimSpace(h.cfg.AntigravityKey[i].APIKey) == val {
				matchCount++
				if matchIndex == -1 {
					matchIndex = i
				}
			}
		}
		if matchCount == 0 {
			c.JSON(404, gin.H{"error": "item not found"})
			return
		}
		if matchCount > 1 {
			c.JSON(400, gin.H{"error": "multiple items match api-key; index is required"})
			return
		}
		h.cfg.AntigravityKey = append(h.cfg.AntigravityKey[:matchIndex], h.cfg.AntigravityKey[matchIndex+1:]...)
		h.cfg.SanitizeAntigravityKeys()
		h.persistLocked(c)
		return
	}
	if idxStr := c.Query("index"); idxStr != "" {
		var idx int
		if _, err := fmt.Sscanf(idxStr, "%d", &idx); err == nil && idx >= 0 && idx < len(h.cfg.AntigravityKey) {
			h.cfg.AntigravityKey = append(h.cfg.AntigravityKey[:idx], h.cfg.AntigravityKey[idx+1:]...)
			h.cfg.SanitizeAntigravityKeys()
			h.persistLocked(c)
			return
		}
	}
	c.JSON(400, gin.H{"error": "missing api-key or index"})
}
