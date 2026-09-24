package config

import "testing"

func TestValidateMaxConcurrentConnections(t *testing.T) {
	if err := ValidateMaxConcurrentConnections(nil); err != nil {
		t.Fatalf("nil = %v", err)
	}
	zero := 0
	if err := ValidateMaxConcurrentConnections(&zero); err != nil {
		t.Fatalf("zero = %v", err)
	}
	positive := 4
	if err := ValidateMaxConcurrentConnections(&positive); err != nil {
		t.Fatalf("positive = %v", err)
	}
	negative := -1
	if err := ValidateMaxConcurrentConnections(&negative); err == nil {
		t.Fatal("negative value accepted")
	}
	over := MaxConcurrentConnectionsLimit + 1
	if err := ValidateMaxConcurrentConnections(&over); err == nil {
		t.Fatal("over-max value accepted")
	}
}

func TestParseConfigBytesRejectsMaxConcurrentConnections(t *testing.T) {
	raw := []byte(`
gemini-api-key:
  - api-key: "gemini-key"
    max-concurrent-connections: -1
`)
	if _, err := ParseConfigBytes(raw); err == nil {
		t.Fatal("expected negative connection limit to be rejected")
	}
	over := []byte(`
claude-api-key:
  - api-key: "claude-key"
    max-concurrent-connections: 1000001
`)
	if _, err := ParseConfigBytes(over); err == nil {
		t.Fatal("expected over-max connection limit to be rejected")
	}
}

func TestParseConfigBytesOmitsUnlimitedMaxConcurrentConnections(t *testing.T) {
	raw := []byte(`
gemini-api-key:
  - api-key: "gemini-unset"
  - api-key: "gemini-zero"
    max-concurrent-connections: 0
  - api-key: "gemini-set"
    max-concurrent-connections: 4
`)
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.GeminiKey[0].MaxConcurrentConnections != nil {
		t.Fatalf("omitted limit = %v, want nil", cfg.GeminiKey[0].MaxConcurrentConnections)
	}
	if cfg.GeminiKey[1].MaxConcurrentConnections == nil || *cfg.GeminiKey[1].MaxConcurrentConnections != 0 {
		t.Fatalf("zero limit = %v, want 0", cfg.GeminiKey[1].MaxConcurrentConnections)
	}
	if cfg.GeminiKey[2].MaxConcurrentConnections == nil || *cfg.GeminiKey[2].MaxConcurrentConnections != 4 {
		t.Fatalf("set limit = %v, want 4", cfg.GeminiKey[2].MaxConcurrentConnections)
	}
}
