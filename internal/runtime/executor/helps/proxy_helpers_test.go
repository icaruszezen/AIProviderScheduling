package helps

import (
	"context"
	"net/http"
	"testing"

	"github.com/icaruszezen/AIProviderScheduling/v7/internal/config"
	apsauth "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/auth"
	sdkconfig "github.com/icaruszezen/AIProviderScheduling/v7/sdk/config"
)

func TestNewProxyAwareHTTPClientDirectBypassesGlobalProxy(t *testing.T) {
	t.Parallel()

	client := NewProxyAwareHTTPClient(
		context.Background(),
		&config.Config{SDKConfig: sdkconfig.SDKConfig{ProxyURL: "http://global-proxy.example.com:8080"}},
		&apsauth.Auth{ProxyURL: "direct"},
		0,
	)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected direct transport to disable proxy function")
	}
}
