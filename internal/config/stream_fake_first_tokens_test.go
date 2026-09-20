package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeStreamFakeFirstTokens(t *testing.T) {
	if got := SanitizeStreamFakeFirstTokens(nil); got != nil {
		t.Fatalf("nil = %v, want nil", got)
	}
	if got := SanitizeStreamFakeFirstTokens([]string{}); got != nil {
		t.Fatalf("empty = %v, want nil", got)
	}
	if got := SanitizeStreamFakeFirstTokens([]string{"", ""}); got != nil {
		t.Fatalf("only blanks = %v, want nil", got)
	}

	got := SanitizeStreamFakeFirstTokens([]string{" ", "-", " ", "", "hello"})
	want := []string{" ", "-", "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sanitized = %#v, want %#v", got, want)
	}

	long := strings.Repeat("x", MaxStreamFakeFirstTokenRunes+1)
	if got = SanitizeStreamFakeFirstTokens([]string{long, "-"}); !reflect.DeepEqual(got, []string{"-"}) {
		t.Fatalf("over-long token = %#v, want [\"-\"]", got)
	}

	many := make([]string, MaxStreamFakeFirstTokens+5)
	for i := range many {
		many[i] = strings.Repeat("a", i+1)
	}
	got = SanitizeStreamFakeFirstTokens(many)
	if len(got) != MaxStreamFakeFirstTokens {
		t.Fatalf("capped length = %d, want %d", len(got), MaxStreamFakeFirstTokens)
	}
}

func TestParseConfigBytesStreamFakeFirstTokens(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte(`
codex-api-key:
  - api-key: "codex-probe"
    base-url: "https://codex.example.com"
    stream-fake-first-tokens: [" ", "-", ""]
  - api-key: "codex-plain"
    base-url: "https://codex.example.com"
xai-api-key:
  - api-key: "xai-key"
    base-url: "https://api.x.ai/v1"
    stream-fake-first-tokens: [" "]
`))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}
	if len(cfg.CodexKey) != 2 {
		t.Fatalf("codex-api-key count = %d, want 2", len(cfg.CodexKey))
	}
	if !reflect.DeepEqual(cfg.CodexKey[0].StreamFakeFirstTokens, []string{" ", "-"}) {
		t.Fatalf("codex[0].stream-fake-first-tokens = %#v, want [\" \", \"-\"]", cfg.CodexKey[0].StreamFakeFirstTokens)
	}
	if cfg.CodexKey[1].StreamFakeFirstTokens != nil {
		t.Fatalf("codex[1].stream-fake-first-tokens = %#v, want nil", cfg.CodexKey[1].StreamFakeFirstTokens)
	}
	if !reflect.DeepEqual(cfg.XAIKey[0].StreamFakeFirstTokens, []string{" "}) {
		t.Fatalf("xai[0].stream-fake-first-tokens = %#v, want [\" \"]", cfg.XAIKey[0].StreamFakeFirstTokens)
	}
}
