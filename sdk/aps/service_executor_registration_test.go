package aps

import (
	"context"
	"net/http"
	"testing"

	runtimeexecutor "github.com/icaruszezen/AIProviderScheduling/v7/internal/runtime/executor"
	coreauth "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/auth"
	apsexecutor "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/executor"
	"github.com/icaruszezen/AIProviderScheduling/v7/sdk/config"
)

type serviceTestPluginExecutor struct{}
type serviceTestSDKExecutor struct{ serviceTestPluginExecutor }

func (serviceTestSDKExecutor) Identifier() string { return "sdk-provider" }

func (serviceTestPluginExecutor) Identifier() string {
	return "plugin-provider"
}

func (serviceTestPluginExecutor) Execute(context.Context, *coreauth.Auth, apsexecutor.Request, apsexecutor.Options) (apsexecutor.Response, error) {
	return apsexecutor.Response{}, nil
}

func (serviceTestPluginExecutor) ExecuteStream(context.Context, *coreauth.Auth, apsexecutor.Request, apsexecutor.Options) (*apsexecutor.StreamResult, error) {
	return nil, nil
}

func (serviceTestPluginExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (serviceTestPluginExecutor) CountTokens(context.Context, *coreauth.Auth, apsexecutor.Request, apsexecutor.Options) (apsexecutor.Response, error) {
	return apsexecutor.Response{}, nil
}

func (serviceTestPluginExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRegisterAvailableExecutors(t *testing.T) {
	service := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil)}
	service.ensureWebsocketGateway()

	service.registerAvailableExecutors(nil, executorRegistrationOptions{
		includeBaseline: true})

	providers := []string{
		"codex",
		"claude",
		"gemini",
		"gemini-interactions",
		"vertex",
		"aistudio",
		"antigravity",
		"kimi",
		"xai",
		"openai-compatibility"}
	for _, provider := range providers {
		resolved, ok := service.coreManager.Executor(provider)
		if !ok || resolved == nil {
			t.Fatalf("expected executor for provider %s after registration", provider)
		}
	}
}

func TestSyncPluginModelRuntimePreservesSDKExecutorUnlessForced(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	custom := serviceTestSDKExecutor{}
	manager.RegisterExecutor(custom)
	auth := &coreauth.Auth{ID: "private-auth", Provider: custom.Identifier()}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: &config.Config{}, coreManager: manager}

	service.registerExecutorForAuth(auth, true)
	got, ok := manager.Executor(custom.Identifier())
	if !ok {
		t.Fatal("forced registration removed executor")
	}
	if _, replaced := got.(*runtimeexecutor.OpenAICompatExecutor); !replaced {
		t.Fatalf("forced registration kept %T, want *executor.OpenAICompatExecutor", got)
	}
}

func TestRegisterExecutorForAuth_OpenAICompatUsesNamespacedProviderKey(t *testing.T) {
	testCases := []struct {
		name  string
		auths []*coreauth.Auth
	}{
		{
			name: "native first",
			auths: []*coreauth.Auth{
				{ID: "native-kimi", Provider: "kimi"},
				openAICompatKimiAuth()}},
		{
			name: "compat first",
			auths: []*coreauth.Auth{
				openAICompatKimiAuth(),
				{ID: "native-kimi", Provider: "kimi"}}}}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{
				cfg:         &config.Config{},
				coreManager: coreauth.NewManager(nil, nil, nil)}

			service.registerExecutorsForAuths(tt.auths, true)

			nativeExecutor, okNative := service.coreManager.Executor("kimi")
			if !okNative {
				t.Fatal("expected native kimi executor")
			}
			if _, okKimi := nativeExecutor.(*runtimeexecutor.KimiExecutor); !okKimi {
				t.Fatalf("native executor type = %T, want *executor.KimiExecutor", nativeExecutor)
			}

			compatExecutor, okCompat := service.coreManager.Executor("openai-compatible-kimi")
			if !okCompat {
				t.Fatal("expected namespaced OpenAI-compatible executor")
			}
			if _, okOpenAICompat := compatExecutor.(*runtimeexecutor.OpenAICompatExecutor); !okOpenAICompat {
				t.Fatalf("compat executor type = %T, want *executor.OpenAICompatExecutor", compatExecutor)
			}
		})
	}
}

func openAICompatKimiAuth() *coreauth.Auth {
	return &coreauth.Auth{
		ID:       "compat-kimi",
		Provider: "openai-compatibility",
		Label:    "kimi",
		Attributes: map[string]string{
			"compat_name":  "kimi",
			"provider_key": "kimi"}}
}
