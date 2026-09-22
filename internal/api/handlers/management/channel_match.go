package management

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func rejectDuplicateChannelNames(c *gin.Context, section string, names []string) bool {
	if err := config.ValidateUniqueChannelNames(section, names); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return true
	}
	return false
}

func findNamedChannel[T any](items []T, name string, nameOf func(T) string) (int, int) {
	trimmed := strings.TrimSpace(name)
	index := -1
	count := 0
	for i := range items {
		if strings.TrimSpace(nameOf(items[i])) != trimmed {
			continue
		}
		count++
		if index < 0 {
			index = i
		}
	}
	return index, count
}

func rejectNamedChannelLookup(c *gin.Context, count int) bool {
	if count == 0 {
		c.JSON(404, gin.H{"error": "item not found"})
		return true
	}
	if count > 1 {
		c.JSON(400, gin.H{"error": "multiple items match name"})
		return true
	}
	return false
}

func countAPIKeyBaseMatches[T any](items []T, apiKey, baseURL string, apiKeyOf func(T) string, baseOf func(T) string) int {
	count := 0
	for i := range items {
		if strings.TrimSpace(apiKeyOf(items[i])) == apiKey && strings.TrimSpace(baseOf(items[i])) == baseURL {
			count++
		}
	}
	return count
}
