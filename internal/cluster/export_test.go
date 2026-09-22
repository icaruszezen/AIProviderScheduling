package cluster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestExportStripsLocalIdentityAndToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Host = "127.0.0.1"
	cfg.Port = 8317
	cfg.APIKeys = []string{"client-key-1"}
	cfg.GeminiKey = []config.GeminiKey{{APIKey: "gemini-secret"}}
	cfg.RemoteManagement.SecretKey = "mgmt-secret"
	cfg.Cluster = config.ClusterConfig{
		Role:         config.ClusterRoleMaster,
		Token:        "super-secret-cluster-token",
		NodeID:       "master-1",
		MasterURL:    "http://should-not-export",
		AdvertiseURL: "http://also-local"}
	cfg.Plugins.Dir = "/opt/local-plugins"
	cfg.Plugins.Enabled = true
	cfg.ChannelMonitor.Enabled = true
	cfg.ChannelMonitor.DatabasePath = "/var/lib/node-local-channel-monitor.db"

	payload, err := Export(cfg)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if payload.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if payload.SourceNodeID != "master-1" {
		t.Fatalf("SourceNodeID = %q", payload.SourceNodeID)
	}
	if containsSecret(payload.ConfigYAML, "super-secret-cluster-token") {
		t.Fatal("exported YAML leaked cluster token")
	}
	if strings.Contains(payload.ConfigYAML, "127.0.0.1") {
		t.Fatal("exported YAML leaked host")
	}
	if strings.Contains(payload.ConfigYAML, "/opt/local-plugins") {
		t.Fatal("exported YAML leaked plugins.dir")
	}
	if strings.Contains(payload.ConfigYAML, "node-local-channel-monitor.db") {
		t.Fatal("exported YAML leaked channel-monitor database-path")
	}
	if !strings.Contains(payload.ConfigYAML, "channel-monitor:") {
		t.Fatal("expected channel-monitor settings to be exported")
	}
	if !strings.Contains(payload.ConfigYAML, "client-key-1") {
		t.Fatal("expected inbound api-keys to be exported")
	}
	if !strings.Contains(payload.ConfigYAML, "gemini-secret") {
		t.Fatal("expected provider api key to be exported")
	}

	again, err := Export(cfg)
	if err != nil {
		t.Fatalf("second Export() error = %v", err)
	}
	if again.Hash != payload.Hash {
		t.Fatalf("hash unstable: %s vs %s", payload.Hash, again.Hash)
	}
}

// TestHashConvergesAcrossNodes covers Apply's core invariant: after a slave
// parses and merges a master export, hashing its own config must reproduce the
// master's hash. If it does not, the hash-equal shortcut never fires and every
// sync tick rewrites config.yaml and reloads the whole runtime.
func TestHashConvergesAcrossNodes(t *testing.T) {
	// Both sides come from LoadConfig so the test exercises the same
	// normalization the real nodes run.
	masterPath := filepath.Join(t.TempDir(), "config.yaml")
	masterYAML := "host: 0.0.0.0\n" +
		"port: 8317\n" +
		"request-retry: 4\n" +
		"api-keys:\n  - client-b\n  - client-a\n" +
		"gemini-api-key:\n" +
		"  - api-key: gemini-1\n" +
		"    provider-retry-count: 2\n" +
		"    provider-retry-status-codes: [429, 500]\n" +
		"openai-compatibility:\n" +
		"  - name: compat\n" +
		"    base-url: https://compat.example.com\n" +
		"    api-key-entries:\n      - api-key: sk-1\n" +
		"plugins:\n  enabled: true\n  dir: /master/plugins\n" +
		"cluster:\n  role: master\n  node-id: master-1\n  token: " +
		strings.Repeat("m", config.MinClusterTokenLength) + "\n"
	if err := os.WriteFile(masterPath, []byte(masterYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	master, err := config.LoadConfig(masterPath)
	if err != nil {
		t.Fatalf("LoadConfig master: %v", err)
	}

	payload, err := Export(master)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	slavePath := filepath.Join(t.TempDir(), "config.yaml")
	slaveYAML := "host: 10.0.0.9\n" +
		"port: 9000\n" +
		"plugins:\n  dir: /slave/plugins\n" +
		"cluster:\n  role: slave\n  node-id: slave-1\n  master-url: http://master:8317\n  token: " +
		strings.Repeat("s", config.MinClusterTokenLength) + "\n"
	if err = os.WriteFile(slavePath, []byte(slaveYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	slave, err := config.LoadConfig(slavePath)
	if err != nil {
		t.Fatalf("LoadConfig slave: %v", err)
	}

	// The slave-side path: parse the exported YAML, merge its local identity back
	// in, then hash the result the way Apply does on the next tick.
	incoming, err := config.ParseConfigBytes([]byte(payload.ConfigYAML))
	if err != nil {
		t.Fatalf("ParseConfigBytes: %v", err)
	}
	merged := MergeLocal(slave, incoming)
	appliedHash, err := HashOf(merged)
	if err != nil {
		t.Fatalf("HashOf: %v", err)
	}
	if appliedHash != payload.Hash {
		t.Fatalf("hash did not converge:\n master=%s\n slave =%s", payload.Hash, appliedHash)
	}

	// A second round must be a no-op, so normalization has to be idempotent.
	secondIncoming, err := config.ParseConfigBytes([]byte(payload.ConfigYAML))
	if err != nil {
		t.Fatalf("second ParseConfigBytes: %v", err)
	}
	secondHash, err := HashOf(MergeLocal(merged, secondIncoming))
	if err != nil {
		t.Fatalf("second HashOf: %v", err)
	}
	if secondHash != payload.Hash {
		t.Fatalf("hash unstable on re-apply: %s vs %s", payload.Hash, secondHash)
	}
}

func TestMergeLocalPreservesIdentityAndAppliesProviders(t *testing.T) {
	local := &config.Config{}
	local.Host = "10.0.0.8"
	local.Port = 9000
	local.APIKeys = []string{"old-key"}
	local.RemoteManagement.AllowRemote = false
	local.RemoteManagement.SecretKey = "local-mgmt"
	local.Cluster = config.ClusterConfig{
		Role:      config.ClusterRoleSlave,
		Token:     "shared-token",
		NodeID:    "slave-1",
		MasterURL: "http://master:8317"}
	local.Plugins.Dir = "/slave/plugins"
	local.ChannelMonitor.DatabasePath = "/slave/channel-monitor.db"
	local.ChannelMonitor.Enabled = false

	incoming := &config.Config{}
	incoming.Host = "should-ignore"
	incoming.Port = 1
	incoming.APIKeys = []string{"synced-key"}
	incoming.GeminiKey = []config.GeminiKey{{APIKey: "synced-gemini"}}
	incoming.RequestRetry = 3
	incoming.Cluster.Token = "must-not-win"
	incoming.Plugins.Dir = "plugins"
	incoming.Plugins.Enabled = true
	incoming.ChannelMonitor.Enabled = true
	incoming.ChannelMonitor.DatabasePath = "/master/channel-monitor.db"

	merged := MergeLocal(local, incoming)
	if merged.Host != "10.0.0.8" || merged.Port != 9000 {
		t.Fatalf("host/port overwritten: %s:%d", merged.Host, merged.Port)
	}
	if merged.RemoteManagement.SecretKey != "local-mgmt" {
		t.Fatal("remote-management overwritten")
	}
	if merged.Cluster.Role != config.ClusterRoleSlave || merged.Cluster.Token != "shared-token" || merged.Cluster.NodeID != "slave-1" {
		t.Fatalf("cluster identity overwritten: %+v", merged.Cluster)
	}
	if merged.Plugins.Dir != "/slave/plugins" {
		t.Fatalf("plugins.dir overwritten: %s", merged.Plugins.Dir)
	}
	if len(merged.APIKeys) != 1 || merged.APIKeys[0] != "synced-key" {
		t.Fatalf("api-keys not synced: %#v", merged.APIKeys)
	}
	if len(merged.GeminiKey) != 1 || merged.GeminiKey[0].APIKey != "synced-gemini" {
		t.Fatalf("gemini keys not synced: %#v", merged.GeminiKey)
	}
	if merged.RequestRetry != 3 {
		t.Fatalf("request-retry not synced: %d", merged.RequestRetry)
	}
	if !merged.ChannelMonitor.Enabled {
		t.Fatal("channel-monitor.enabled was not synced")
	}
	if merged.ChannelMonitor.DatabasePath != "/slave/channel-monitor.db" {
		t.Fatalf("database-path = %q", merged.ChannelMonitor.DatabasePath)
	}
}

func TestApplyDoesNotTouchAuthFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	authDir := filepath.Join(dir, "auths")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(authDir, "account.json")
	if err := os.WriteFile(authPath, []byte(`{"email":"local@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("host: 10.0.0.2\nport: 8317\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	local := &config.Config{}
	local.Host = "10.0.0.2"
	local.Port = 8317
	local.Cluster = config.ClusterConfig{Role: config.ClusterRoleSlave, Token: "tok", NodeID: "s1", MasterURL: "http://127.0.0.1:1"}

	incoming := &config.Config{}
	incoming.APIKeys = []string{"from-master"}
	merged := MergeLocal(local, incoming)
	if err := config.SaveConfigPreserveComments(configPath, merged); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("auth file missing: %v", err)
	}
	if !strings.Contains(string(data), "local@example.com") {
		t.Fatalf("auth file changed: %s", data)
	}
	entries, err := os.ReadDir(authDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("auth dir mutated, entries=%d", len(entries))
	}
}

func TestLoadConfigGeneratesNodeID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("cluster:\n  role: slave\n  token: tok\n  master-url: http://127.0.0.1:8317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Cluster.NodeID == "" {
		t.Fatal("expected generated node-id")
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), cfg.Cluster.NodeID) {
		t.Fatalf("node-id not persisted: %s", onDisk)
	}
}
