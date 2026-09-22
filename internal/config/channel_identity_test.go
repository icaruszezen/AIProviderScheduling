package config

import "testing"

func TestSanitizeGeminiKeysKeepsSameURLAndAPIKeyWhenNamesDiffer(t *testing.T) {
	cfg := &Config{
		GeminiKey: []GeminiKey{
			{Name: "tokyo", APIKey: "shared", BaseURL: "https://gemini.example", Group: "  生产  "},
			{Name: "osaka", APIKey: "shared", BaseURL: "https://gemini.example"},
			{APIKey: "shared", BaseURL: "https://gemini.example"},
		},
	}
	cfg.SanitizeGeminiKeys()
	if len(cfg.GeminiKey) != 3 {
		t.Fatalf("gemini keys = %d, want 3", len(cfg.GeminiKey))
	}
	if cfg.GeminiKey[0].Group != "生产" {
		t.Fatalf("group = %q, want 生产", cfg.GeminiKey[0].Group)
	}
	if cfg.GeminiKey[1].Group != "" || cfg.GeminiKey[2].Name != "" {
		t.Fatalf("legacy channel was rewritten: %#v", cfg.GeminiKey)
	}
	if err := cfg.ValidateChannelNames(); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeGeminiKeysCollapsesUnnamedDuplicates(t *testing.T) {
	cfg := &Config{
		GeminiKey: []GeminiKey{
			{APIKey: "shared", BaseURL: "https://gemini.example", ProxyURL: "http://proxy", Prefix: "g", Headers: map[string]string{"X-A": "1"}},
			{APIKey: "shared", BaseURL: "https://gemini.example", ProxyURL: "http://proxy", Prefix: "g", Headers: map[string]string{"X-A": "1"}},
			{Name: "tokyo", APIKey: "shared", BaseURL: "https://gemini.example", ProxyURL: "http://proxy", Prefix: "g", Headers: map[string]string{"X-A": "1"}},
		},
	}
	cfg.SanitizeGeminiKeys()
	if len(cfg.GeminiKey) != 2 {
		t.Fatalf("gemini keys = %d, want 2", len(cfg.GeminiKey))
	}
	if cfg.GeminiKey[0].Name != "" || cfg.GeminiKey[1].Name != "tokyo" {
		t.Fatalf("remaining = %#v", cfg.GeminiKey)
	}
}

func TestSanitizeAntigravityKeysCollapsesOnlyUnnamedDuplicates(t *testing.T) {
	cfg := &Config{
		AntigravityKey: []AntigravityKey{
			{APIKey: "shared", BaseURL: "https://ag.example", ProjectID: "p1"},
			{APIKey: "shared", BaseURL: "https://ag.example", ProjectID: "p1"},
			{Name: "tokyo", APIKey: "shared", BaseURL: "https://ag.example", ProjectID: "p1"},
		},
	}
	cfg.SanitizeAntigravityKeys()
	if len(cfg.AntigravityKey) != 2 || cfg.AntigravityKey[1].Name != "tokyo" {
		t.Fatalf("antigravity keys = %#v", cfg.AntigravityKey)
	}
}

func TestSanitizeVertexCompatKeysCollapsesOnlyUnnamedDuplicates(t *testing.T) {
	account := map[string]any{"client_email": "vertex@example.iam.gserviceaccount.com"}
	cfg := &Config{
		VertexCompatAPIKey: []VertexCompatKey{
			{APIKey: "shared", BaseURL: "https://vertex.example", ServiceAccount: account, ProxyURL: "http://a"},
			{APIKey: "shared", BaseURL: "https://vertex.example", ServiceAccount: account, ProxyURL: "http://b"},
			{Name: "tokyo", APIKey: "shared", BaseURL: "https://vertex.example", ServiceAccount: account},
		},
	}
	cfg.SanitizeVertexCompatKeys()
	if len(cfg.VertexCompatAPIKey) != 2 || cfg.VertexCompatAPIKey[1].Name != "tokyo" {
		t.Fatalf("vertex keys = %#v", cfg.VertexCompatAPIKey)
	}
}

func TestValidateChannelNamesRejectsDuplicates(t *testing.T) {
	cfg := &Config{CodexKey: configCodexKeys()}
	if err := cfg.ValidateChannelNames(); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

func TestValidateChannelNamesFoldsOpenAICompatCase(t *testing.T) {
	cfg := &Config{OpenAICompatibility: []OpenAICompatibility{
		{Name: "Foo", BaseURL: "https://a.example"},
		{Name: "foo", BaseURL: "https://b.example"},
	}}
	if err := cfg.ValidateChannelNames(); err == nil {
		t.Fatal("expected case-insensitive openai name error")
	}
	caseDistinct := &Config{CodexKey: []CodexKey{
		{Name: "Foo", APIKey: "a", BaseURL: "https://codex.example"},
		{Name: "foo", APIKey: "b", BaseURL: "https://codex.example"},
	}}
	if err := caseDistinct.ValidateChannelNames(); err != nil {
		t.Fatal(err)
	}
}

func configCodexKeys() []CodexKey {
	return []CodexKey{
		{Name: "tokyo", APIKey: "a", BaseURL: "https://codex.example"},
		{Name: "tokyo", APIKey: "b", BaseURL: "https://other.example"},
	}
}

func TestNormalizeChannelGroupsDropsBlanksAndDuplicates(t *testing.T) {
	cfg := &Config{ChannelGroups: map[string][]string{
		" codex ": {" 生产 ", "", "生产", "备用"},
	}}
	cfg.NormalizeChannelGroups()
	got := cfg.ChannelGroups["codex"]
	if len(got) != 2 || got[0] != "生产" || got[1] != "备用" {
		t.Fatalf("groups = %#v", cfg.ChannelGroups)
	}
}

func TestUsageCompositeKey(t *testing.T) {
	if got := UsageCompositeKey("https://a", "key", ""); got != "https://a|key" {
		t.Fatalf("legacy key = %q", got)
	}
	named := UsageCompositeKey("https://a", "key", "tokyo")
	other := UsageCompositeKey("https://a", "key", "osaka")
	if named == other || named == "https://a|key" {
		t.Fatalf("named keys were not distinct: %q %q", named, other)
	}
}
