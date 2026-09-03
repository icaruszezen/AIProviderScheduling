package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/cluster"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

type stubCluster struct {
	status        cluster.NodeSelfStatus
	nodes         []cluster.NodeStatus
	registerErr   error
	heartbeatErr  error
	exportPayload *cluster.ExportPayload
	exportErr     error
	applyErr      error
	syncErr       error
	syncResults   []cluster.PushResult
	applied       *cluster.ExportPayload
	registered    []cluster.Registration
	heartbeats    []cluster.Heartbeat
}

func (s *stubCluster) Status() cluster.NodeSelfStatus { return s.status }
func (s *stubCluster) Nodes() []cluster.NodeStatus    { return s.nodes }
func (s *stubCluster) Register(req cluster.Registration) error {
	s.registered = append(s.registered, req)
	return s.registerErr
}
func (s *stubCluster) Heartbeat(req cluster.Heartbeat) error {
	s.heartbeats = append(s.heartbeats, req)
	return s.heartbeatErr
}
func (s *stubCluster) Unregister(string) {}
func (s *stubCluster) RemoveNode(string) {}
func (s *stubCluster) Export() (*cluster.ExportPayload, error) {
	return s.exportPayload, s.exportErr
}
func (s *stubCluster) Apply(_ context.Context, payload *cluster.ExportPayload) error {
	s.applied = payload
	return s.applyErr
}
func (s *stubCluster) SyncNow(context.Context) ([]cluster.PushResult, error) {
	return s.syncResults, s.syncErr
}

func TestSlaveWriteGuardBlocksConfigYAML(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(&config.Config{Cluster: config.ClusterConfig{Role: config.ClusterRoleSlave}}, configPath, nil)
	engine := gin.New()
	engine.PUT("/v0/management/config.yaml", h.SlaveConfigWriteGuard(), h.PutConfigYAML)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v0/management/config.yaml", bytes.NewBufferString("port: 9\n"))
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSlaveWriteGuardAllowsAuthFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{cfg: &config.Config{Cluster: config.ClusterConfig{Role: config.ClusterRoleSlave}}}
	engine := gin.New()
	engine.POST("/v0/management/auth-files", h.SlaveConfigWriteGuard(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

// testClusterToken satisfies config.MinClusterTokenLength.
const testClusterToken = "cluster-token-0123456789abcdefghij"

func TestClusterRegisterAndExport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubCluster{
		exportPayload: &cluster.ExportPayload{Protocol: 1, Hash: "abc", ConfigYAML: "api-keys: [k]\n"},
	}
	h := NewHandler(&config.Config{Cluster: config.ClusterConfig{Role: config.ClusterRoleMaster, Token: testClusterToken}}, "", nil)
	h.SetClusterController(stub)
	engine := gin.New()
	engine.POST("/v0/management/cluster/register", h.ClusterTokenMiddleware(), h.PostClusterRegister)
	engine.GET("/v0/management/cluster/export", h.ClusterTokenMiddleware(), h.GetClusterExport)

	regBody, _ := json.Marshal(cluster.Registration{NodeID: "n1", AdvertiseURL: "http://slave:8317"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v0/management/cluster/register", bytes.NewReader(regBody))
	req.Header.Set("Authorization", "Bearer "+testClusterToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.registered) != 1 || stub.registered[0].NodeID != "n1" {
		t.Fatalf("registered=%#v", stub.registered)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v0/management/cluster/export", nil)
	req.Header.Set("Authorization", "Bearer "+testClusterToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSlavePersistLockedRejects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(&config.Config{Port: 8317, Cluster: config.ClusterConfig{Role: config.ClusterRoleSlave}}, configPath, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/debug", nil)
	if h.persist(c) {
		t.Fatal("expected persist to reject slave writes")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostClusterSyncReportsPartialFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubCluster{syncResults: []cluster.PushResult{
		{NodeID: "n1", Status: cluster.PushStatusPushed},
		{NodeID: "n2", Status: cluster.PushStatusFailed, Error: "dial tcp: refused"},
	}}
	h := NewHandler(&config.Config{}, "", nil)
	h.SetClusterController(stub)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/cluster/sync", nil)
	h.PostClusterSync(ctx)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Status string               `json:"status"`
		Failed int                  `json:"failed"`
		Nodes  []cluster.PushResult `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "partial" || got.Failed != 1 || len(got.Nodes) != 2 {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestPostClusterSyncReportsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubCluster{syncResults: []cluster.PushResult{{NodeID: "n1", Status: cluster.PushStatusPushed}}}
	h := NewHandler(&config.Config{}, "", nil)
	h.SetClusterController(stub)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/cluster/sync", nil)
	h.PostClusterSync(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSlaveWriteAllowedPluginPaths(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodPost, "/v0/management/plugin-store/foo/install", true},
		{http.MethodDelete, "/v0/management/plugins/foo", true},
		{http.MethodDelete, "/v0/management/plugins/foo/config", false},
		{http.MethodPatch, "/v0/management/plugins/foo/enabled", false},
		{http.MethodPut, "/v0/management/plugins/foo/config", false},
		{http.MethodDelete, "/v0/management/plugins/", false},
		{http.MethodDelete, "/v0/management/gemini-api-key", false},
	}
	for _, tc := range tests {
		if got := slaveWriteAllowed(tc.method, tc.path); got != tc.want {
			t.Errorf("slaveWriteAllowed(%s, %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestPatchClusterRejectsUnusableSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
		code string
	}{
		{name: "unknown role", body: `{"role":"leader"}`, code: "invalid_role"},
		{name: "slave without master", body: `{"role":"slave"}`, code: "master_url_required"},
		{name: "sync interval too small", body: `{"sync_interval_seconds":1}`, code: "invalid_interval"},
		{name: "heartbeat interval too small", body: `{"heartbeat_interval_seconds":2}`, code: "invalid_interval"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/cluster", bytes.NewReader([]byte(tc.body)))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.PatchCluster(ctx)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var got struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Error != tc.code {
				t.Fatalf("error = %q, want %q", got.Error, tc.code)
			}
			// A rejected patch must not leave a half-applied config behind.
			if h.cfg.Cluster.NormalizedRole() != config.ClusterRoleStandalone {
				t.Fatalf("role = %q, want the config untouched", h.cfg.Cluster.Role)
			}
		})
	}
}

func TestPatchClusterAcceptsSlaveWithMaster(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{cfg: &config.Config{}, configFilePath: writeTestConfigFile(t)}
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	body := `{"role":"slave","master_url":"http://master:8317/","sync_interval_seconds":10}`
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/cluster", bytes.NewReader([]byte(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PatchCluster(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !h.cfg.Cluster.IsSlave() {
		t.Fatalf("role = %q, want slave", h.cfg.Cluster.Role)
	}
	if h.cfg.Cluster.MasterURL != "http://master:8317" {
		t.Fatalf("master_url = %q, want the trailing slash trimmed", h.cfg.Cluster.MasterURL)
	}
	if h.cfg.Cluster.SyncIntervalSeconds != 10 {
		t.Fatalf("sync_interval_seconds = %d, want 10", h.cfg.Cluster.SyncIntervalSeconds)
	}
}

func TestClusterTokenRejectsWrongSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(&config.Config{Cluster: config.ClusterConfig{Token: testClusterToken}}, "", nil)
	engine := gin.New()
	engine.GET("/v0/management/cluster/export", h.ClusterTokenMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/cluster/export", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestClusterTokenRejectsWeakSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	weak := "tok"
	h := NewHandler(&config.Config{Cluster: config.ClusterConfig{Token: weak}}, "", nil)
	engine := gin.New()
	engine.GET("/v0/management/cluster/export", h.ClusterTokenMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/cluster/export", nil)
	req.Header.Set("Authorization", "Bearer "+weak)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("weak token accepted: status=%d", rec.Code)
	}
}

func TestClusterTokenBansAfterRepeatedFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(&config.Config{Cluster: config.ClusterConfig{Token: testClusterToken}}, "", nil)
	engine := gin.New()
	engine.GET("/v0/management/cluster/export", h.ClusterTokenMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := 0; i < attemptMaxFailures; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/cluster/export", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d, want 401", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/cluster/export", nil)
	req.Header.Set("Authorization", "Bearer "+testClusterToken)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected ban after %d failures, status=%d", attemptMaxFailures, rec.Code)
	}
}
