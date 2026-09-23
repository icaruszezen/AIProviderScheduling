package config

import "testing"

func TestValidateStreamFirstTokenTimeout(t *testing.T) {
	if err := ValidateStreamFirstTokenTimeout(nil); err != nil {
		t.Fatalf("nil = %v", err)
	}
	zero := 0
	if err := ValidateStreamFirstTokenTimeout(&zero); err != nil {
		t.Fatalf("zero = %v", err)
	}
	positive := 15
	if err := ValidateStreamFirstTokenTimeout(&positive); err != nil {
		t.Fatalf("positive = %v", err)
	}
	negative := -1
	if err := ValidateStreamFirstTokenTimeout(&negative); err == nil {
		t.Fatal("negative value accepted")
	}
	over := MaxStreamFirstTokenTimeoutSeconds + 1
	if err := ValidateStreamFirstTokenTimeout(&over); err == nil {
		t.Fatal("over-max value accepted")
	}
}

func TestParseConfigBytesRejectsStreamFirstTokenTimeout(t *testing.T) {
	raw := []byte(`
gemini-api-key:
  - api-key: "gemini-key"
    stream-first-token-timeout-seconds: 3601
`)
	if _, err := ParseConfigBytes(raw); err == nil {
		t.Fatal("expected out-of-range timeout to be rejected")
	}
}

func TestParseConfigBytesOmitsDisabledStreamFirstTokenTimeout(t *testing.T) {
	raw := []byte(`
gemini-api-key:
  - api-key: "gemini-unset"
  - api-key: "gemini-zero"
    stream-first-token-timeout-seconds: 0
  - api-key: "gemini-set"
    stream-first-token-timeout-seconds: 12
`)
	cfg, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds != nil {
		t.Fatalf("omitted timeout = %v, want nil", cfg.GeminiKey[0].StreamFirstTokenTimeoutSeconds)
	}
	if cfg.GeminiKey[1].StreamFirstTokenTimeoutSeconds == nil || *cfg.GeminiKey[1].StreamFirstTokenTimeoutSeconds != 0 {
		t.Fatalf("zero timeout = %v, want 0", cfg.GeminiKey[1].StreamFirstTokenTimeoutSeconds)
	}
	if cfg.GeminiKey[2].StreamFirstTokenTimeoutSeconds == nil || *cfg.GeminiKey[2].StreamFirstTokenTimeoutSeconds != 12 {
		t.Fatalf("set timeout = %v, want 12", cfg.GeminiKey[2].StreamFirstTokenTimeoutSeconds)
	}
}
