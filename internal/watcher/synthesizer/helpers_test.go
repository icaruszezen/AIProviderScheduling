package synthesizer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/diff"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestNewStableIDGenerator(t *testing.T) {
	gen := NewStableIDGenerator()
	if gen == nil {
		t.Fatal("expected non-nil generator")
	}
	if gen.counters == nil {
		t.Fatal("expected non-nil counters map")
	}
}

func TestStableIDGenerator_Next(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		parts      []string
		wantPrefix string
	}{
		{
			name:       "basic gemini apikey",
			kind:       "gemini:apikey",
			parts:      []string{"test-key", ""},
			wantPrefix: "gemini:apikey:"},
		{
			name:       "claude with base url",
			kind:       "claude:apikey",
			parts:      []string{"sk-ant-xxx", "https://api.anthropic.com"},
			wantPrefix: "claude:apikey:"},
		{
			name:       "empty parts",
			kind:       "codex:apikey",
			parts:      []string{},
			wantPrefix: "codex:apikey:"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := NewStableIDGenerator()
			id, short := gen.Next(tt.kind, tt.parts...)

			if !strings.Contains(id, tt.wantPrefix) {
				t.Errorf("expected id to contain %q, got %q", tt.wantPrefix, id)
			}
			if short == "" {
				t.Error("expected non-empty short id")
			}
			if len(short) != 12 {
				t.Errorf("expected short id length 12, got %d", len(short))
			}
		})
	}
}

func TestStableIDGenerator_Stability(t *testing.T) {
	gen1 := NewStableIDGenerator()
	gen2 := NewStableIDGenerator()

	id1, _ := gen1.Next("gemini:apikey", "test-key", "https://api.example.com")
	id2, _ := gen2.Next("gemini:apikey", "test-key", "https://api.example.com")

	if id1 != id2 {
		t.Errorf("same inputs should produce same ID: got %q and %q", id1, id2)
	}
}

func TestStableIDGenerator_CollisionHandling(t *testing.T) {
	gen := NewStableIDGenerator()

	id1, short1 := gen.Next("gemini:apikey", "same-key")
	id2, short2 := gen.Next("gemini:apikey", "same-key")

	if id1 == id2 {
		t.Error("collision should be handled with suffix")
	}
	if short1 == short2 {
		t.Error("short ids should differ")
	}
	if !strings.Contains(short2, "-1") {
		t.Errorf("second short id should contain -1 suffix, got %q", short2)
	}
}

func TestStableIDGenerator_NilReceiver(t *testing.T) {
	var gen *StableIDGenerator = nil
	id, short := gen.Next("test:kind", "part")

	if id != "test:kind:000000000000" {
		t.Errorf("expected test:kind:000000000000, got %q", id)
	}
	if short != "000000000000" {
		t.Errorf("expected 000000000000, got %q", short)
	}
}

func TestApplyAuthExcludedModelsMeta(t *testing.T) {
	tests := []struct {
		name     string
		auth     *coreauth.Auth
		cfg      *config.Config
		perKey   []string
		authKind string
		wantHash bool
		wantKind string
	}{
		{
			name: "apikey with excluded models",
			auth: &coreauth.Auth{
				Provider:   "gemini",
				Attributes: make(map[string]string)},
			cfg:      &config.Config{},
			perKey:   []string{"model-a", "model-b"},
			authKind: "apikey",
			wantHash: true,
			wantKind: "apikey"},
		{
			name: "oauth ignores provider excluded models",
			auth: &coreauth.Auth{
				Provider:   "claude",
				Attributes: make(map[string]string)},
			cfg:      &config.Config{},
			perKey:   nil,
			authKind: "oauth",
			wantHash: false,
			wantKind: "oauth"},
		{
			name: "nil auth",
			auth: nil,
			cfg:  &config.Config{}},
		{
			name:     "nil config",
			auth:     &coreauth.Auth{Provider: "test"},
			cfg:      nil,
			authKind: "apikey"},
		{
			name: "nil attributes initialized",
			auth: &coreauth.Auth{
				Provider:   "gemini",
				Attributes: nil},
			cfg:      &config.Config{},
			perKey:   []string{"model-x"},
			authKind: "apikey",
			wantHash: true,
			wantKind: "apikey"},
		{
			name: "apikey with duplicate excluded models",
			auth: &coreauth.Auth{
				Provider:   "gemini",
				Attributes: make(map[string]string)},
			cfg:      &config.Config{},
			perKey:   []string{"model-a", "MODEL-A", "model-b", "model-a"},
			authKind: "apikey",
			wantHash: true,
			wantKind: "apikey"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ApplyAuthExcludedModelsMeta(tt.auth, tt.cfg, tt.perKey, tt.authKind)

			if tt.auth != nil && tt.cfg != nil {
				if tt.wantHash {
					if _, ok := tt.auth.Attributes["excluded_models_hash"]; !ok {
						t.Error("expected excluded_models_hash in attributes")
					}
				}
				if tt.wantKind != "" {
					if got := tt.auth.Attributes["auth_kind"]; got != tt.wantKind {
						t.Errorf("expected auth_kind=%s, got %s", tt.wantKind, got)
					}
				}
			}
		})
	}
}

func TestApplyAuthExcludedModelsMeta_IgnoresGlobalOAuthExcludedModels(t *testing.T) {
	auth := &coreauth.Auth{
		Provider:   "claude",
		Attributes: make(map[string]string)}
	cfg := &config.Config{}

	ApplyAuthExcludedModelsMeta(auth, cfg, []string{"per", "SHARED"}, "oauth")

	const wantCombined = "per,shared"
	if gotCombined := auth.Attributes["excluded_models"]; gotCombined != wantCombined {
		t.Fatalf("expected excluded_models=%q, got %q", wantCombined, gotCombined)
	}

	expectedHash := diff.ComputeExcludedModelsHash([]string{"per", "shared"})
	if gotHash := auth.Attributes["excluded_models_hash"]; gotHash != expectedHash {
		t.Fatalf("expected excluded_models_hash=%q, got %q", expectedHash, gotHash)
	}
}

func TestAddConfigHeadersToAttrs(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		attrs   map[string]string
		want    map[string]string
	}{
		{
			name: "basic headers",
			headers: map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "value"},
			attrs: map[string]string{"existing": "key"},
			want: map[string]string{
				"existing":             "key",
				"header:Authorization": "Bearer token",
				"header:X-Custom":      "value"}},
		{
			name:    "empty headers",
			headers: map[string]string{},
			attrs:   map[string]string{"existing": "key"},
			want:    map[string]string{"existing": "key"}},
		{
			name:    "nil headers",
			headers: nil,
			attrs:   map[string]string{"existing": "key"},
			want:    map[string]string{"existing": "key"}},
		{
			name:    "nil attrs",
			headers: map[string]string{"key": "value"},
			attrs:   nil,
			want:    nil},
		{
			name: "skip empty keys and values",
			headers: map[string]string{
				"":      "value",
				"key":   "",
				"  ":    "value",
				"valid": "valid-value"},
			attrs: make(map[string]string),
			want: map[string]string{
				"header:valid": "valid-value"}}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addConfigHeadersToAttrs(tt.headers, tt.attrs)
			if !reflect.DeepEqual(tt.attrs, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, tt.attrs)
			}
		})
	}
}

func TestAddRequestRetryToMetadata(t *testing.T) {
	zero := 0
	positive := 2
	negative := -1

	metadata := map[string]any{}
	addRequestRetryToMetadata(&zero, metadata)
	if got, ok := metadata["request_retry"].(int); !ok || got != 0 {
		t.Fatalf("zero request-retry = %v, want 0", metadata["request_retry"])
	}

	metadata = map[string]any{}
	addRequestRetryToMetadata(&positive, metadata)
	if got, ok := metadata["request_retry"].(int); !ok || got != 2 {
		t.Fatalf("positive request-retry = %v, want 2", metadata["request_retry"])
	}

	metadata = map[string]any{}
	addRequestRetryToMetadata(&negative, metadata)
	if _, exists := metadata["request_retry"]; exists {
		t.Fatalf("negative request-retry should be omitted, got %v", metadata["request_retry"])
	}

	metadata = map[string]any{}
	addRequestRetryToMetadata(nil, metadata)
	if _, exists := metadata["request_retry"]; exists {
		t.Fatalf("nil request-retry should be omitted, got %v", metadata["request_retry"])
	}

	addRequestRetryToMetadata(&positive, nil)
}

func TestAddStreamFirstTokenTimeoutToMetadata(t *testing.T) {
	zero := 0
	positive := 15
	negative := -1

	metadata := map[string]any{}
	addStreamFirstTokenTimeoutToMetadata(&positive, metadata)
	if got, ok := metadata["stream_first_token_timeout_seconds"].(int); !ok || got != 15 {
		t.Fatalf("positive timeout = %v, want 15", metadata["stream_first_token_timeout_seconds"])
	}

	metadata = map[string]any{}
	addStreamFirstTokenTimeoutToMetadata(&zero, metadata)
	if _, exists := metadata["stream_first_token_timeout_seconds"]; exists {
		t.Fatalf("zero timeout should be omitted, got %v", metadata["stream_first_token_timeout_seconds"])
	}

	metadata = map[string]any{}
	addStreamFirstTokenTimeoutToMetadata(&negative, metadata)
	addStreamFirstTokenTimeoutToMetadata(nil, metadata)
	if _, exists := metadata["stream_first_token_timeout_seconds"]; exists {
		t.Fatalf("disabled timeout should be omitted, got %v", metadata["stream_first_token_timeout_seconds"])
	}

	addStreamFirstTokenTimeoutToMetadata(&positive, nil)
}

func TestAddProviderRetryToMetadata(t *testing.T) {
	zero := 0
	positive := 3
	negative := -1
	codes := []int{401, 429}
	empty := []int{}

	metadata := map[string]any{}
	addProviderRetryToMetadata(&zero, nil, metadata)
	if got, ok := metadata["provider_retry_count"].(int); !ok || got != 0 {
		t.Fatalf("zero count = %v, want 0", metadata["provider_retry_count"])
	}
	if _, exists := metadata["provider_retry_status_codes"]; exists {
		t.Fatalf("nil codes should be omitted")
	}

	metadata = map[string]any{}
	addProviderRetryToMetadata(&positive, &codes, metadata)
	if got, ok := metadata["provider_retry_count"].(int); !ok || got != 3 {
		t.Fatalf("positive count = %v, want 3", metadata["provider_retry_count"])
	}
	gotCodes, ok := metadata["provider_retry_status_codes"].([]int)
	if !ok || !reflect.DeepEqual(gotCodes, []int{401, 429}) {
		t.Fatalf("codes = %v, want [401 429]", metadata["provider_retry_status_codes"])
	}

	metadata = map[string]any{}
	addProviderRetryToMetadata(&positive, &empty, metadata)
	gotCodes, ok = metadata["provider_retry_status_codes"].([]int)
	if !ok || len(gotCodes) != 0 {
		t.Fatalf("empty codes = %v, want empty slice", metadata["provider_retry_status_codes"])
	}

	metadata = map[string]any{}
	addProviderRetryToMetadata(&negative, &codes, metadata)
	if _, exists := metadata["provider_retry_count"]; exists {
		t.Fatalf("negative count should be omitted")
	}

	addProviderRetryToMetadata(&positive, &codes, nil)
}

func TestAddHideNoAvailableChannelToMetadata(t *testing.T) {
	metadata := map[string]any{}
	addHideNoAvailableChannelToMetadata(false, metadata)
	if _, exists := metadata["hide_no_available_channel"]; exists {
		t.Fatal("false hide switch should be omitted")
	}
	addHideNoAvailableChannelToMetadata(true, metadata)
	if got, ok := metadata["hide_no_available_channel"].(bool); !ok || !got {
		t.Fatalf("hide_no_available_channel = %#v, want true", metadata["hide_no_available_channel"])
	}
	addHideNoAvailableChannelToMetadata(true, nil)
}

func TestAddStreamFakeFirstTokensToMetadata(t *testing.T) {
	metadata := map[string]any{}
	addStreamFakeFirstTokensToMetadata(nil, metadata)
	if _, exists := metadata["stream_fake_first_tokens"]; exists {
		t.Fatal("empty tokens should be omitted")
	}
	addStreamFakeFirstTokensToMetadata([]string{" ", "-", " "}, metadata)
	got, ok := metadata["stream_fake_first_tokens"].([]string)
	if !ok || len(got) != 2 || got[0] != " " || got[1] != "-" {
		t.Fatalf("stream_fake_first_tokens = %#v, want [\" \", \"-\"]", metadata["stream_fake_first_tokens"])
	}
	addStreamFakeFirstTokensToMetadata([]string{"-"}, nil)
}
