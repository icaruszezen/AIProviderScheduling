package cluster

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"gopkg.in/yaml.v3"
)

const protocolVersion = 1

// ExportPayload is the filtered config snapshot exchanged between master and slave.
type ExportPayload struct {
	Protocol      int       `json:"protocol"`
	Hash          string    `json:"hash"`
	UpdatedAt     time.Time `json:"updated_at"`
	SourceNodeID  string    `json:"source_node_id,omitempty"`
	SourceVersion string    `json:"source_version,omitempty"`
	ConfigYAML    string    `json:"config_yaml"`
}

// StripLocalIdentity returns a clone with per-node identity fields cleared.
func StripLocalIdentity(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	out := cfg.CloneForRuntime()
	if out == nil {
		return nil
	}
	out.Host = ""
	out.Port = 0
	out.TLS = config.TLSConfig{}
	out.RemoteManagement = config.RemoteManagement{}
	out.Cluster = config.ClusterConfig{}
	out.Home = config.HomeConfig{}
	out.Pprof = config.PprofConfig{}
	out.Plugins.Dir = ""
	out.ChannelMonitor.DatabasePath = ""
	return out
}

// MergeLocal copies incoming synced fields and restores local identity from local.
func MergeLocal(local, incoming *config.Config) *config.Config {
	if incoming == nil {
		if local == nil {
			return nil
		}
		return local.CloneForRuntime()
	}
	out := incoming.CloneForRuntime()
	if local == nil {
		return out
	}
	preserved := local.CloneForRuntime()
	out.Host = preserved.Host
	out.Port = preserved.Port
	out.TLS = preserved.TLS
	out.RemoteManagement = preserved.RemoteManagement
	out.Cluster = preserved.Cluster
	out.Home = preserved.Home
	out.Pprof = preserved.Pprof
	out.Plugins.Dir = preserved.Plugins.Dir
	out.ChannelMonitor.DatabasePath = preserved.ChannelMonitor.DatabasePath
	return out
}

// Export builds a protocol payload from cfg. Token and other local identity fields are stripped.
func Export(cfg *config.Config) (*ExportPayload, error) {
	stripped := StripLocalIdentity(cfg)
	yamlBytes, hash, err := CanonicalYAML(stripped)
	if err != nil {
		return nil, err
	}
	sourceNodeID := ""
	if cfg != nil {
		sourceNodeID = strings.TrimSpace(cfg.Cluster.NodeID)
	}
	return &ExportPayload{
		Protocol:      protocolVersion,
		Hash:          hash,
		UpdatedAt:     time.Now().UTC(),
		SourceNodeID:  sourceNodeID,
		SourceVersion: buildinfo.Version,
		ConfigYAML:    string(yamlBytes)}, nil
}

// HashOf returns the canonical hash of cfg after stripping local identity.
func HashOf(cfg *config.Config) (string, error) {
	_, hash, err := CanonicalYAML(StripLocalIdentity(cfg))
	return hash, err
}

// CanonicalYAML marshals cfg with sorted mapping keys for a stable hash.
func CanonicalYAML(cfg *config.Config) ([]byte, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("cluster: config is nil")
	}
	rendered, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("cluster: marshal config: %w", err)
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(rendered, &doc); err != nil {
		return nil, "", fmt.Errorf("cluster: parse generated yaml: %w", err)
	}
	sortYAMLNode(&doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err = enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, "", fmt.Errorf("cluster: encode canonical yaml: %w", err)
	}
	if err = enc.Close(); err != nil {
		return nil, "", fmt.Errorf("cluster: close canonical yaml encoder: %w", err)
	}
	canonical := buf.Bytes()
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

func sortYAMLNode(node *yaml.Node) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode || node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			sortYAMLNode(child)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	type pair struct {
		key   *yaml.Node
		value *yaml.Node
	}
	pairs := make([]pair, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		pairs = append(pairs, pair{key: node.Content[i], value: node.Content[i+1]})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].key.Value < pairs[j].key.Value
	})
	node.Content = node.Content[:0]
	for _, item := range pairs {
		sortYAMLNode(item.key)
		sortYAMLNode(item.value)
		node.Content = append(node.Content, item.key, item.value)
	}
}

func containsSecret(payload string, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" || payload == "" {
		return false
	}
	return strings.Contains(payload, secret)
}
