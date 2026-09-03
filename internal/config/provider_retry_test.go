package config

import (
	"reflect"
	"testing"
)

func TestSanitizeProviderRetryCount(t *testing.T) {
	if got := SanitizeProviderRetryCount(nil); got != nil {
		t.Fatalf("nil count = %v, want nil", got)
	}

	neg := -1
	if got := SanitizeProviderRetryCount(&neg); got != nil {
		t.Fatalf("negative count = %v, want nil", got)
	}

	zero := 0
	if got := SanitizeProviderRetryCount(&zero); got == nil || *got != 0 {
		t.Fatalf("zero count = %v, want 0", got)
	}

	over := 99
	if got := SanitizeProviderRetryCount(&over); got == nil || *got != MaxProviderRetryCount {
		t.Fatalf("over-max count = %v, want %d", got, MaxProviderRetryCount)
	}
}

func TestSanitizeProviderRetryStatusCodes(t *testing.T) {
	if got := SanitizeProviderRetryStatusCodes(nil); got != nil {
		t.Fatalf("nil codes = %v, want nil", got)
	}

	empty := []int{}
	gotEmpty := SanitizeProviderRetryStatusCodes(&empty)
	if gotEmpty == nil || len(*gotEmpty) != 0 {
		t.Fatalf("explicit empty = %v, want empty slice", gotEmpty)
	}

	raw := []int{429, 99, 401, 429, 600, 403}
	got := SanitizeProviderRetryStatusCodes(&raw)
	want := []int{401, 403, 429}
	if got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("sanitized codes = %v, want %v", got, want)
	}
}

func TestParseConfigBytesProviderRetry(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte(`
gemini-api-key:
  - api-key: "gemini-retry"
    provider-retry-count: 12
    provider-retry-status-codes: [429, 401, 99, 429]
  - api-key: "gemini-empty"
    provider-retry-count: 2
    provider-retry-status-codes: []
  - api-key: "gemini-unset"
  - api-key: "gemini-neg"
    provider-retry-count: -3
openai-compatibility:
  - name: "compat"
    base-url: "https://compat.example.com/v1"
    provider-retry-count: 3
    api-key-entries:
      - api-key: "compat-key"
vertex-api-key:
  - api-key: "vertex-retry"
    provider-retry-count: 1
    provider-retry-status-codes: [500, 502]
`))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}

	if len(cfg.GeminiKey) != 4 {
		t.Fatalf("gemini-api-key count = %d, want 4", len(cfg.GeminiKey))
	}
	if cfg.GeminiKey[0].ProviderRetryCount == nil || *cfg.GeminiKey[0].ProviderRetryCount != MaxProviderRetryCount {
		t.Fatalf("gemini[0].provider-retry-count = %v, want %d", cfg.GeminiKey[0].ProviderRetryCount, MaxProviderRetryCount)
	}
	if cfg.GeminiKey[0].ProviderRetryStatusCodes == nil || !reflect.DeepEqual(*cfg.GeminiKey[0].ProviderRetryStatusCodes, []int{401, 429}) {
		t.Fatalf("gemini[0].provider-retry-status-codes = %v, want [401 429]", cfg.GeminiKey[0].ProviderRetryStatusCodes)
	}
	if cfg.GeminiKey[1].ProviderRetryStatusCodes == nil || len(*cfg.GeminiKey[1].ProviderRetryStatusCodes) != 0 {
		t.Fatalf("gemini[1].provider-retry-status-codes = %v, want empty", cfg.GeminiKey[1].ProviderRetryStatusCodes)
	}
	if cfg.GeminiKey[2].ProviderRetryCount != nil || cfg.GeminiKey[2].ProviderRetryStatusCodes != nil {
		t.Fatalf("gemini[2] should leave retry fields unset")
	}
	if cfg.GeminiKey[3].ProviderRetryCount != nil {
		t.Fatalf("gemini[3].provider-retry-count = %v, want unset", cfg.GeminiKey[3].ProviderRetryCount)
	}
	if len(cfg.OpenAICompatibility) != 1 || cfg.OpenAICompatibility[0].ProviderRetryCount == nil || *cfg.OpenAICompatibility[0].ProviderRetryCount != 3 {
		t.Fatalf("openai-compatibility[0].provider-retry-count = %v, want 3", cfg.OpenAICompatibility[0].ProviderRetryCount)
	}
	if len(cfg.VertexCompatAPIKey) != 1 || cfg.VertexCompatAPIKey[0].ProviderRetryCount == nil || *cfg.VertexCompatAPIKey[0].ProviderRetryCount != 1 {
		t.Fatalf("vertex[0].provider-retry-count = %v, want 1", cfg.VertexCompatAPIKey[0].ProviderRetryCount)
	}
	if cfg.VertexCompatAPIKey[0].ProviderRetryStatusCodes == nil || !reflect.DeepEqual(*cfg.VertexCompatAPIKey[0].ProviderRetryStatusCodes, []int{500, 502}) {
		t.Fatalf("vertex[0].provider-retry-status-codes = %v, want [500 502]", cfg.VertexCompatAPIKey[0].ProviderRetryStatusCodes)
	}
}
