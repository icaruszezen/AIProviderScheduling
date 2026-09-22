package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestOpenAIModelsClientVersionUsesChannelGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		insideModel  = "group-client-version-inside"
		outsideModel = "group-client-version-outside"
		insideID     = "group-client-version-inside-auth"
		outsideID    = "group-client-version-outside-auth"
	)
	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(insideID, "codex", []*registry.ModelInfo{{ID: insideModel}})
	modelRegistry.RegisterClient(outsideID, "codex", []*registry.ModelInfo{{ID: outsideModel}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(insideID)
		modelRegistry.UnregisterClient(outsideID)
	})

	manager := coreauth.NewManager(nil, nil, nil)
	registerGroupModelAuth(t, manager, insideID, "team")
	registerGroupModelAuth(t, manager, outsideID, "other")

	handler := NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager))
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models?client_version=0.149.1", nil)
	ginContext.Set("accessMetadata", map[string]string{
		coreauth.ChannelGroupProviderMetadataKey: "codex",
		coreauth.ChannelGroupMetadataKey:         "team",
	})

	handler.OpenAIModels(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, insideModel) {
		t.Fatalf("group model missing from client catalog: %s", body)
	}
	if strings.Contains(body, outsideModel) {
		t.Fatalf("outside model leaked into client catalog: %s", body)
	}
}

func registerGroupModelAuth(t *testing.T, manager *coreauth.Manager, id, group string) {
	t.Helper()
	if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
		ID:       id,
		Provider: "codex",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			coreauth.AttributeChannelGroup: group,
			coreauth.AttributeChannelPanel: "codex",
		},
	}); errRegister != nil {
		t.Fatalf("Register(%s) error = %v", id, errRegister)
	}
}
