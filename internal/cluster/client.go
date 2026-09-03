package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	clusterAPIPrefix = "/v0/management/cluster"
	clientTimeout    = 15 * time.Second

	// applyTimeout covers /apply, where the peer writes config.yaml and reloads
	// its whole runtime before responding.
	applyTimeout = 120 * time.Second

	// maxResponseBytes caps a peer response. The exported config grows with the
	// number of credentials, so this has to be well above a typical config.
	maxResponseBytes = 32 << 20
)

// Client talks to a peer node's cluster protocol endpoints.
type Client struct {
	http *http.Client
}

// NewClient builds a peer client. Timeouts are applied per request rather than
// on the shared http.Client, because /apply needs far more headroom than the
// short control-plane calls.
func NewClient() *Client {
	return &Client{http: &http.Client{}}
}

func (c *Client) Register(ctx context.Context, masterURL, token string, req Registration) error {
	return c.postJSON(ctx, masterURL, token, "/register", req, nil)
}

func (c *Client) Heartbeat(ctx context.Context, masterURL, token string, req Heartbeat) error {
	return c.postJSON(ctx, masterURL, token, "/heartbeat", req, nil)
}

func (c *Client) Unregister(ctx context.Context, masterURL, token, nodeID string) error {
	body := map[string]string{"node_id": strings.TrimSpace(nodeID)}
	return c.postJSON(ctx, masterURL, token, "/unregister", body, nil)
}

func (c *Client) Export(ctx context.Context, masterURL, token string) (*ExportPayload, error) {
	var payload ExportPayload
	if err := c.doJSON(ctx, http.MethodGet, masterURL, token, "/export", nil, &payload); err != nil {
		return nil, err
	}
	if payload.Protocol != 0 && payload.Protocol != protocolVersion {
		return nil, fmt.Errorf("cluster: unsupported protocol %d", payload.Protocol)
	}
	return &payload, nil
}

func (c *Client) Apply(ctx context.Context, slaveURL, token string, payload *ExportPayload) error {
	if payload == nil {
		return fmt.Errorf("cluster: apply payload is nil")
	}
	// The peer persists and reloads inside this request, so it needs more headroom
	// than the short control-plane calls.
	return c.request(ctx, applyTimeout, http.MethodPut, slaveURL, token, "/apply", payload, nil)
}

func (c *Client) postJSON(ctx context.Context, baseURL, token, path string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPost, baseURL, token, path, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, baseURL, token, path string, body any, out any) error {
	return c.request(ctx, clientTimeout, method, baseURL, token, path, body, out)
}

func (c *Client) request(ctx context.Context, timeout time.Duration, method, baseURL, token, path string, body any, out any) error {
	if c == nil || c.http == nil {
		return fmt.Errorf("cluster: http client is nil")
	}
	endpoint, err := joinClusterURL(baseURL, path)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return fmt.Errorf("cluster: marshal request: %w", errMarshal)
		}
		reader = bytes.NewReader(encoded)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("cluster: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Cluster-Token", token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cluster: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Read one byte past the cap so an oversized response reports itself instead
	// of failing later as a truncated-JSON decode error.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if len(respBody) > maxResponseBytes {
		return fmt.Errorf("cluster: %s %s: response exceeds %d bytes", method, path, maxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("cluster: %s %s: %s", method, path, msg)
	}
	if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err = json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("cluster: decode %s: %w", path, err)
	}
	return nil
}

func joinClusterURL(baseURL, path string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("cluster: peer url is empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("cluster: invalid peer url %q", baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("cluster: peer url must be http or https")
	}
	return strings.TrimRight(baseURL, "/") + clusterAPIPrefix + path, nil
}
