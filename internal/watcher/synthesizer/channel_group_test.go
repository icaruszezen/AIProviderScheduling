package synthesizer

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestSynthesizeChannelGroupPanel(t *testing.T) {
	cfg := &config.Config{
		CodexKey: []config.CodexKey{{
			Name:    "tokyo",
			APIKey:  "codex-key",
			BaseURL: "https://codex.example",
			Group:   "team",
		}},
		ClaudeKey: []config.ClaudeKey{{
			Name:    "kimi",
			APIKey:  "kimi-key",
			BaseURL: "https://api.moonshot.cn/anthropic",
			Group:   "batch",
		}, {
			Name:    "claude-moonshot-v1",
			APIKey:  "claude-moonshot-v1-key",
			BaseURL: "https://api.moonshot.cn/v1/",
			Group:   "batch",
		}},
		GeminiKey: []config.GeminiKey{{
			Name:    "lmu",
			APIKey:  "lmu-gemini-key",
			BaseURL: "https://api.lmuai.com",
			Group:   "vision",
		}, {
			Name:    "custom-lmu-host",
			APIKey:  "custom-lmu-key",
			BaseURL: "https://api.lmuai.com/custom",
			Group:   "vision",
		}},
	}
	auths, errSynthesize := NewConfigSynthesizer().Synthesize(&SynthesisContext{
		Config:      cfg,
		Now:         time.Unix(0, 0),
		IDGenerator: NewStableIDGenerator(),
	})
	if errSynthesize != nil {
		t.Fatal(errSynthesize)
	}
	found := map[string]*coreauth.Auth{}
	for _, auth := range auths {
		if auth == nil || auth.Attributes == nil {
			continue
		}
		found[auth.Attributes["api_key"]] = auth
	}
	codex := found["codex-key"]
	if codex == nil || codex.Attributes[coreauth.AttributeChannelGroup] != "team" || codex.Attributes[coreauth.AttributeChannelPanel] != "codex" {
		t.Fatalf("codex auth = %#v", codex)
	}
	kimi := found["kimi-key"]
	if kimi == nil || kimi.Attributes[coreauth.AttributeChannelGroup] != "batch" || kimi.Attributes[coreauth.AttributeChannelPanel] != "kimi" {
		t.Fatalf("kimi auth = %#v", kimi)
	}
	claudePath := found["claude-moonshot-v1-key"]
	if claudePath == nil || claudePath.Attributes[coreauth.AttributeChannelPanel] != "claude" {
		t.Fatalf("non-canonical claude auth = %#v", claudePath)
	}
	lmu := found["lmu-gemini-key"]
	if lmu == nil || lmu.Attributes[coreauth.AttributeChannelPanel] != "lmu-ai" {
		t.Fatalf("lmu gemini auth = %#v", lmu)
	}
	custom := found["custom-lmu-key"]
	if custom == nil || custom.Attributes[coreauth.AttributeChannelPanel] != "gemini" {
		t.Fatalf("custom lmu host auth = %#v", custom)
	}
}
