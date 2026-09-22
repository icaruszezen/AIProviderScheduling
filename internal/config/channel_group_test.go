package config

import (
	"strings"
	"testing"
)

func TestChannelGroupAcceptsStringAndObject(t *testing.T) {
	const payload = `
channel-groups:
  codex:
    - team
    - name: production
      api-keys:
        - " sk-group "
      channel-retry-count: 2
      channel-retry-status-codes: [429, 500, 429]
      channel-retry-error-contains:
        - " overloaded "
        - overloaded
`
	cfg, errParse := ParseConfigBytes([]byte(payload))
	if errParse != nil {
		t.Fatal(errParse)
	}
	groups := cfg.ChannelGroups["codex"]
	if len(groups) != 2 || groups[0].Name != "team" || groups[1].Name != "production" {
		t.Fatalf("groups = %#v", groups)
	}
	if len(groups[1].APIKeys) != 1 || groups[1].APIKeys[0] != "sk-group" {
		t.Fatalf("api keys = %#v", groups[1].APIKeys)
	}
	if groups[1].ChannelRetryLimit() != 2 {
		t.Fatalf("retry = %d", groups[1].ChannelRetryLimit())
	}
	if len(groups[1].ChannelRetryStatusCodes) != 2 || groups[1].ChannelRetryStatusCodes[0] != 429 || groups[1].ChannelRetryStatusCodes[1] != 500 {
		t.Fatalf("status codes = %#v", groups[1].ChannelRetryStatusCodes)
	}
	if len(groups[1].ChannelRetryErrorContains) != 1 || groups[1].ChannelRetryErrorContains[0] != "overloaded" {
		t.Fatalf("errors = %#v", groups[1].ChannelRetryErrorContains)
	}
	creds := cfg.ChannelGroupCredentials()
	if creds["sk-group"].Panel != "codex" || creds["sk-group"].Group != "production" {
		t.Fatalf("credential = %#v", creds["sk-group"])
	}
}

func TestNormalizeChannelGroupsRejectsDuplicateAndGlobalKeys(t *testing.T) {
	duplicate := &Config{ChannelGroups: map[string][]ChannelGroup{
		"codex":  {{Name: "a", APIKeys: []string{"sk-1"}}},
		"claude": {{Name: "b", APIKeys: []string{"sk-1"}}},
	}}
	if err := duplicate.NormalizeChannelGroups(); err == nil {
		t.Fatal("expected duplicate group key error")
	}
	global := &Config{ChannelGroups: map[string][]ChannelGroup{
		"codex": {{Name: "a", APIKeys: []string{" sk-global "}}},
	}}
	global.APIKeys = []string{"sk-global"}
	if err := global.NormalizeChannelGroups(); err == nil {
		t.Fatal("expected global api key conflict")
	}
	blank := &Config{ChannelGroups: map[string][]ChannelGroup{
		"codex": {{Name: "a", APIKeys: []string{"  "}}},
	}}
	if err := blank.NormalizeChannelGroups(); err == nil {
		t.Fatal("expected empty api key error")
	}
}

func TestNormalizeChannelGroupsRejectsDuplicateNames(t *testing.T) {
	cfg := &Config{ChannelGroups: map[string][]ChannelGroup{
		" codex ": {{Name: "production"}, {Name: " production ", APIKeys: []string{"sk-live"}}},
		"codex":   {{Name: "other"}},
	}}
	err := cfg.NormalizeChannelGroups()
	if err == nil {
		t.Fatal("expected duplicate group name error")
	}
	message := err.Error()
	if !strings.Contains(message, `"production"`) || !strings.Contains(message, `"codex"`) {
		t.Fatalf("error = %q", message)
	}
}
