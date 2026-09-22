package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestConfigSynthesizer_SameURLAndAPIKeyStayDistinctWhenNamed(t *testing.T) {
	synth := NewConfigSynthesizer()
	ctx := &SynthesisContext{
		Config: &config.Config{
			CodexKey: []config.CodexKey{
				{Name: "tokyo", APIKey: "shared", BaseURL: "https://codex.example"},
				{Name: "osaka", APIKey: "shared", BaseURL: "https://codex.example"},
				{APIKey: "legacy", BaseURL: "https://codex.example"},
			},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}
	auths, err := synth.Synthesize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 3 {
		t.Fatalf("auths = %d, want 3", len(auths))
	}
	if auths[0].ID == auths[1].ID {
		t.Fatalf("named channels share auth id %s", auths[0].ID)
	}
	if auths[0].Attributes["channel_name"] != "tokyo" || auths[1].Attributes["channel_name"] != "osaka" {
		t.Fatalf("channel names = %#v %#v", auths[0].Attributes, auths[1].Attributes)
	}
	if _, exists := auths[2].Attributes["channel_name"]; exists {
		t.Fatal("unnamed channel should keep the legacy auth id inputs")
	}

	legacy := &SynthesisContext{
		Config: &config.Config{
			CodexKey: []config.CodexKey{{APIKey: "legacy", BaseURL: "https://codex.example"}},
		},
		Now:         time.Now(),
		IDGenerator: NewStableIDGenerator(),
	}
	legacyAuths, err := synth.Synthesize(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if legacyAuths[0].ID != auths[2].ID {
		t.Fatalf("unnamed auth id changed: %s vs %s", legacyAuths[0].ID, auths[2].ID)
	}
}
