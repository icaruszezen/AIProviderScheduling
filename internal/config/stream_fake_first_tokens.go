package config

import "unicode/utf8"

const (
	// MaxStreamFakeFirstTokens bounds how many probe tokens a credential may declare.
	MaxStreamFakeFirstTokens = 32
	// MaxStreamFakeFirstTokenRunes bounds a single probe token. Upstream fakes are
	// typically one character; longer values would never match a first delta.
	MaxStreamFakeFirstTokenRunes = 32
)

// SanitizeStreamFakeFirstTokens drops empty strings, exact duplicates, and
// over-long entries without trimming (a single space must remain a space).
// An empty result is returned as nil so omitempty stays omitted.
func SanitizeStreamFakeFirstTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if utf8.RuneCountInString(token) > MaxStreamFakeFirstTokenRunes {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
		if len(out) >= MaxStreamFakeFirstTokens {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
