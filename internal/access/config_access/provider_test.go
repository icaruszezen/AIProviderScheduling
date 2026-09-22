package configaccess

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestAuthenticateGroupKeyAddsMetadataAndGlobalKeyDoesNot(t *testing.T) {
	provider := newProvider("config-inline", []string{"global-key"}, map[string]GroupCredential{
		"group-key": {Panel: "codex", Group: "team", PolicyJSON: `{"count":1}`},
	})

	globalReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	globalReq.Header.Set("Authorization", "Bearer global-key")
	globalResult, globalErr := provider.Authenticate(t.Context(), globalReq)
	if globalErr != nil {
		t.Fatal(globalErr)
	}
	if _, exists := globalResult.Metadata[coreauth.ChannelGroupMetadataKey]; exists {
		t.Fatalf("global metadata = %#v", globalResult.Metadata)
	}

	groupReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	groupReq.Header.Set("X-Api-Key", "group-key")
	groupResult, groupErr := provider.Authenticate(t.Context(), groupReq)
	if groupErr != nil {
		t.Fatal(groupErr)
	}
	if groupResult.Metadata[coreauth.ChannelGroupProviderMetadataKey] != "codex" || groupResult.Metadata[coreauth.ChannelGroupMetadataKey] != "team" {
		t.Fatalf("group metadata = %#v", groupResult.Metadata)
	}
	if groupResult.Provider != sdkaccess.DefaultAccessProviderName {
		t.Fatalf("provider = %q", groupResult.Provider)
	}
}
