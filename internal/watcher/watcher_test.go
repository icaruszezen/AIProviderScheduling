package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/diff"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"gopkg.in/yaml.v3"
)

func TestApplyAuthExcludedModelsMeta_APIKey(t *testing.T) {
	auth := &coreauth.Auth{Attributes: map[string]string{}}
	cfg := &config.Config{}
	perKey := []string{" Model-1 ", "model-2"}

	synthesizer.ApplyAuthExcludedModelsMeta(auth, cfg, perKey, "apikey")

	expected := diff.ComputeExcludedModelsHash([]string{"model-1", "model-2"})
	if got := auth.Attributes["excluded_models_hash"]; got != expected {
		t.Fatalf("expected hash %s, got %s", expected, got)
	}
	if got := auth.Attributes["auth_kind"]; got != "apikey" {
		t.Fatalf("expected auth_kind=apikey, got %s", got)
	}
}

func TestApplyAuthExcludedModelsMeta_OAuthProviderIgnoresGlobalList(t *testing.T) {
	auth := &coreauth.Auth{
		Provider:   "TestProv",
		Attributes: map[string]string{}}
	cfg := &config.Config{}

	synthesizer.ApplyAuthExcludedModelsMeta(auth, cfg, nil, "oauth")

	if got := auth.Attributes["excluded_models_hash"]; got != "" {
		t.Fatalf("expected no global oauth excluded-models hash, got %s", got)
	}
	if got := auth.Attributes["auth_kind"]; got != "oauth" {
		t.Fatalf("expected auth_kind=oauth, got %s", got)
	}
}

func TestBuildAPIKeyClientsCounts(t *testing.T) {
	cfg := &config.Config{
		GeminiKey:       []config.GeminiKey{{APIKey: "g1"}, {APIKey: "g2"}},
		InteractionsKey: []config.GeminiKey{{APIKey: "i1"}},
		VertexCompatAPIKey: []config.VertexCompatKey{
			{APIKey: "v1"}},
		ClaudeKey: []config.ClaudeKey{{APIKey: "c1"}},
		CodexKey:  []config.CodexKey{{APIKey: "c1"}, {APIKey: "c2"}},
		XAIKey:    []config.XAIKey{{APIKey: "x1"}},
		OpenAICompatibility: []config.OpenAICompatibility{
			{APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "o1"}, {APIKey: "o2"}}}},
		AntigravityKey: []config.AntigravityKey{{APIKey: "a1", ProjectID: "p1"}}}

	gemini, vertex, claude, codex, xai, compat, antigravity := BuildAPIKeyClients(cfg)
	if gemini != 3 || vertex != 1 || claude != 1 || codex != 2 || xai != 1 || compat != 2 || antigravity != 1 {
		t.Fatalf("unexpected counts: %d %d %d %d %d %d %d", gemini, vertex, claude, codex, xai, compat, antigravity)
	}
}

func TestNormalizeAuthStripsTemporalFields(t *testing.T) {
	now := time.Now()
	auth := &coreauth.Auth{
		CreatedAt:        now,
		UpdatedAt:        now,
		LastRefreshedAt:  now,
		NextRefreshAfter: now,
		Quota: coreauth.QuotaState{
			NextRecoverAt: now},
		Runtime: map[string]any{"k": "v"}}

	normalized := normalizeAuth(auth)
	if !normalized.CreatedAt.IsZero() || !normalized.UpdatedAt.IsZero() || !normalized.LastRefreshedAt.IsZero() || !normalized.NextRefreshAfter.IsZero() {
		t.Fatal("expected time fields to be zeroed")
	}
	if normalized.Runtime != nil {
		t.Fatal("expected runtime to be nil")
	}
	if !normalized.Quota.NextRecoverAt.IsZero() {
		t.Fatal("expected quota.NextRecoverAt to be zeroed")
	}
}

func TestMatchProvider(t *testing.T) {
	if _, ok := matchProvider("OpenAI", []string{"openai", "claude"}); !ok {
		t.Fatal("expected match to succeed ignoring case")
	}
	if _, ok := matchProvider("missing", []string{"openai"}); ok {
		t.Fatal("expected match to fail for unknown provider")
	}
}

func TestSnapshotCoreAuths_ConfigAndAuthFiles(t *testing.T) {
	authDir := t.TempDir()
	metadata := map[string]any{
		"type":       "gemini",
		"email":      "user@example.com",
		"project_id": "proj-a, proj-b",
		"proxy_url":  "https://proxy"}
	authFile := filepath.Join(authDir, "gemini.json")
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	if err = os.WriteFile(authFile, data, 0o644); err != nil {
		t.Fatalf("failed to write auth file: %v", err)
	}

	cfg := &config.Config{
		GeminiKey: []config.GeminiKey{
			{
				APIKey:         "g-key",
				BaseURL:        "https://gemini",
				ExcludedModels: []string{"Model-A", "model-b"},
				Headers:        map[string]string{"X-Req": "1"}}}}

	w := &Watcher{authDir: authDir}
	w.SetConfig(cfg)

	auths := w.SnapshotCoreAuths()
	if len(auths) != 1 {
		t.Fatalf("expected 1 config auth entry, got %d", len(auths))
	}

	var geminiAPIKeyAuth *coreauth.Auth
	for _, a := range auths {
		if a.Provider == "gemini" && a.Attributes["api_key"] == "g-key" {
			geminiAPIKeyAuth = a
		}
	}
	if geminiAPIKeyAuth == nil {
		t.Fatal("expected synthesized Gemini API key auth")
	}
	expectedAPIKeyHash := diff.ComputeExcludedModelsHash([]string{"Model-A", "model-b"})
	if geminiAPIKeyAuth.Attributes["excluded_models_hash"] != expectedAPIKeyHash {
		t.Fatalf("expected API key excluded hash %s, got %s", expectedAPIKeyHash, geminiAPIKeyAuth.Attributes["excluded_models_hash"])
	}
	if geminiAPIKeyAuth.Attributes["auth_kind"] != "apikey" {
		t.Fatalf("expected auth_kind=apikey, got %s", geminiAPIKeyAuth.Attributes["auth_kind"])
	}
}

func TestReloadConfigIfChanged_TriggersOnChangeAndSkipsUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	writeConfig := func(port int, allowRemote bool) {
		cfg := &config.Config{
			Port:               port,
			CredentialInFlight: config.DefaultCredentialInFlightConfig(),
			RemoteManagement: config.RemoteManagement{
				AllowRemote: allowRemote}}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			t.Fatalf("failed to marshal config: %v", err)
		}
		if err = os.WriteFile(configPath, data, 0o644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}
	}

	writeConfig(8080, false)

	reloads := 0
	w := &Watcher{
		configPath:     configPath,
		authDir:        authDir,
		reloadCallback: func(*config.Config) { reloads++ }}

	w.reloadConfigIfChanged()
	if reloads != 1 {
		t.Fatalf("expected first reload to trigger callback once, got %d", reloads)
	}

	// Same content should be skipped by hash check.
	w.reloadConfigIfChanged()
	if reloads != 1 {
		t.Fatalf("expected unchanged config to be skipped, callback count %d", reloads)
	}

	writeConfig(9090, true)
	w.reloadConfigIfChanged()
	if reloads != 2 {
		t.Fatalf("expected changed config to trigger reload, callback count %d", reloads)
	}
	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()
	if w.config == nil || w.config.Port != 9090 || !w.config.RemoteManagement.AllowRemote {
		t.Fatalf("expected config to be updated after reload, got %+v", w.config)
	}
}

func TestStartAndStopSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("auth_dir: "+authDir), 0o644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	var reloads int32
	w, err := NewWatcher(configPath, authDir, func(*config.Config) {
		atomic.AddInt32(&reloads, 1)
	})
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	w.SetConfig(&config.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("expected Start to succeed: %v", err)
	}
	cancel()
	if err := w.Stop(); err != nil {
		t.Fatalf("expected Stop to succeed: %v", err)
	}
	if got := atomic.LoadInt32(&reloads); got != 1 {
		t.Fatalf("expected one reload callback, got %d", got)
	}
}

func TestStartFailsWhenConfigMissing(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "missing-config.yaml")

	w, err := NewWatcher(configPath, authDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err == nil {
		t.Fatal("expected Start to fail for missing config file")
	}
}

func TestDispatchRuntimeAuthUpdateEnqueuesAndUpdatesState(t *testing.T) {
	queue := make(chan AuthUpdate, 4)
	w := &Watcher{}
	w.SetAuthUpdateQueue(queue)
	defer w.stopDispatch()

	auth := &coreauth.Auth{ID: "auth-1", Provider: "test"}
	if ok := w.DispatchRuntimeAuthUpdate(AuthUpdate{Action: AuthUpdateActionAdd, Auth: auth}); !ok {
		t.Fatal("expected DispatchRuntimeAuthUpdate to enqueue")
	}

	select {
	case update := <-queue:
		if update.Action != AuthUpdateActionAdd || update.Auth.ID != "auth-1" {
			t.Fatalf("unexpected update: %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for auth update")
	}

	if ok := w.DispatchRuntimeAuthUpdate(AuthUpdate{Action: AuthUpdateActionDelete, ID: "auth-1"}); !ok {
		t.Fatal("expected delete update to enqueue")
	}
	select {
	case update := <-queue:
		if update.Action != AuthUpdateActionDelete || update.ID != "auth-1" {
			t.Fatalf("unexpected delete update: %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete update")
	}
	w.clientsMutex.RLock()
	if _, exists := w.runtimeAuths["auth-1"]; exists {
		w.clientsMutex.RUnlock()
		t.Fatal("expected runtime auth to be cleared after delete")
	}
	w.clientsMutex.RUnlock()
}

func TestAuthSliceToMap(t *testing.T) {
	t.Parallel()

	valid1 := &coreauth.Auth{ID: "a"}
	valid2 := &coreauth.Auth{ID: "b"}
	dupOld := &coreauth.Auth{ID: "dup", Label: "old"}
	dupNew := &coreauth.Auth{ID: "dup", Label: "new"}
	empty := &coreauth.Auth{ID: "  "}

	tests := []struct {
		name string
		in   []*coreauth.Auth
		want map[string]*coreauth.Auth
	}{
		{
			name: "nil input",
			in:   nil,
			want: map[string]*coreauth.Auth{}},
		{
			name: "empty input",
			in:   []*coreauth.Auth{},
			want: map[string]*coreauth.Auth{}},
		{
			name: "filters invalid auths",
			in:   []*coreauth.Auth{nil, empty},
			want: map[string]*coreauth.Auth{}},
		{
			name: "keeps valid auths",
			in:   []*coreauth.Auth{valid1, nil, valid2},
			want: map[string]*coreauth.Auth{"a": valid1, "b": valid2}},
		{
			name: "last duplicate wins",
			in:   []*coreauth.Auth{dupOld, dupNew},
			want: map[string]*coreauth.Auth{"dup": dupNew}}}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := authSliceToMap(tc.in)
			if len(tc.want) == 0 {
				if got == nil {
					t.Fatal("expected empty map, got nil")
				}
				if len(got) != 0 {
					t.Fatalf("expected empty map, got %#v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("unexpected map length: got %d, want %d", len(got), len(tc.want))
			}
			for id, wantAuth := range tc.want {
				gotAuth, ok := got[id]
				if !ok {
					t.Fatalf("missing id %q in result map", id)
				}
				if !authEqual(gotAuth, wantAuth) {
					t.Fatalf("unexpected auth for id %q: got %#v, want %#v", id, gotAuth, wantAuth)
				}
			}
		})
	}
}

func TestTriggerServerUpdateCancelsPendingTimerOnImmediate(t *testing.T) {
	cfg := &config.Config{}

	var reloads int32
	w := &Watcher{
		reloadCallback: func(*config.Config) {
			atomic.AddInt32(&reloads, 1)
		}}
	w.SetConfig(cfg)

	w.serverUpdateMu.Lock()
	w.serverUpdateLast = time.Now().Add(-(serverUpdateDebounce - 100*time.Millisecond))
	w.serverUpdateMu.Unlock()
	w.triggerServerUpdate(cfg)

	if got := atomic.LoadInt32(&reloads); got != 0 {
		t.Fatalf("expected no immediate reload, got %d", got)
	}

	w.serverUpdateMu.Lock()
	if !w.serverUpdatePend || w.serverUpdateTimer == nil {
		w.serverUpdateMu.Unlock()
		t.Fatal("expected a pending server update timer")
	}
	w.serverUpdateLast = time.Now().Add(-(serverUpdateDebounce + 10*time.Millisecond))
	w.serverUpdateMu.Unlock()

	w.triggerServerUpdate(cfg)
	if got := atomic.LoadInt32(&reloads); got != 1 {
		t.Fatalf("expected immediate reload once, got %d", got)
	}

	time.Sleep(250 * time.Millisecond)
	if got := atomic.LoadInt32(&reloads); got != 1 {
		t.Fatalf("expected pending timer to be cancelled, got %d reloads", got)
	}
}

func TestReloadClientsLogsConfigDiffs(t *testing.T) {
	tmpDir := t.TempDir()
	oldCfg := &config.Config{Port: 1, Debug: false}
	newCfg := &config.Config{Port: 2, Debug: true}

	w := &Watcher{
		authDir: tmpDir,
		config:  oldCfg}
	w.SetConfig(oldCfg)
	w.oldConfigYaml, _ = yaml.Marshal(oldCfg)

	w.clientsMutex.Lock()
	w.config = newCfg
	w.clientsMutex.Unlock()

	w.reloadClients(false)
}

func TestReloadClientsHandlesNilConfig(t *testing.T) {
	w := &Watcher{}
	w.reloadClients(false)
}

func TestReloadClientsNotifiesUsageSubscribersToRefresh(t *testing.T) {
	tmp := t.TempDir()
	redisqueue.SetEnabled(false)
	redisqueue.SetEnabled(true)
	t.Cleanup(func() { redisqueue.SetEnabled(false) })

	subscriber, unsubscribe := redisqueue.SubscribeUsage()
	defer unsubscribe()
	requireWatcherUsagePayload(t, subscriber, `{"support_refresh":true}`)

	w := &Watcher{
		authDir: tmp,
		config:  &config.Config{}}
	w.reloadClients(false)

	requireWatcherUsagePayload(t, subscriber, `{"refresh":true}`)
}

func requireWatcherUsagePayload(t *testing.T, subscriber <-chan []byte, want string) {
	t.Helper()

	select {
	case got, ok := <-subscriber:
		if !ok {
			t.Fatalf("subscriber closed before receiving %q", want)
		}
		if string(got) != want {
			t.Fatalf("subscriber payload = %q, want %q", string(got), want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for subscriber payload %q", want)
	}
}

func TestSetAuthUpdateQueueNilResetsDispatch(t *testing.T) {
	w := &Watcher{}
	queue := make(chan AuthUpdate, 1)
	w.SetAuthUpdateQueue(queue)
	if w.dispatchCond == nil || w.dispatchCancel == nil {
		t.Fatal("expected dispatch to be initialized")
	}
	w.SetAuthUpdateQueue(nil)
	if w.dispatchCancel != nil {
		t.Fatal("expected dispatch cancel to be cleared when queue nil")
	}
}

func TestPersistAsyncEarlyReturns(t *testing.T) {
	var nilWatcher *Watcher
	nilWatcher.persistConfigAsync()

	w := &Watcher{}
	w.persistConfigAsync()
}

type errorPersister struct {
	configCalls int32
	authCalls   int32
}

func (p *errorPersister) PersistConfig(context.Context) error {
	atomic.AddInt32(&p.configCalls, 1)
	return fmt.Errorf("persist config error")
}

func (p *errorPersister) PersistAuthFiles(context.Context, string, ...string) error {
	atomic.AddInt32(&p.authCalls, 1)
	return fmt.Errorf("persist auth error")
}

func TestPersistAsyncErrorPaths(t *testing.T) {
	p := &errorPersister{}
	w := &Watcher{storePersister: p}
	w.persistConfigAsync()
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&p.configCalls) != 1 {
		t.Fatalf("expected PersistConfig to be called once, got %d", p.configCalls)
	}
	if atomic.LoadInt32(&p.authCalls) != 0 {
		t.Fatalf("expected PersistAuthFiles not to be called, got %d", p.authCalls)
	}
}

func TestStopConfigReloadTimerSafeWhenNil(t *testing.T) {
	w := &Watcher{}
	w.stopConfigReloadTimer()
	w.configReloadMu.Lock()
	w.configReloadTimer = time.AfterFunc(10*time.Millisecond, func() {})
	w.configReloadMu.Unlock()
	time.Sleep(1 * time.Millisecond)
	w.stopConfigReloadTimer()
}

func TestDispatchAuthUpdatesFlushesQueue(t *testing.T) {
	queue := make(chan AuthUpdate, 4)
	w := &Watcher{}
	w.SetAuthUpdateQueue(queue)
	defer w.stopDispatch()

	w.dispatchAuthUpdates([]AuthUpdate{
		{Action: AuthUpdateActionAdd, ID: "a"},
		{Action: AuthUpdateActionModify, ID: "b"}})

	got := make([]AuthUpdate, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case u := <-queue:
			got = append(got, u)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for update %d", i)
		}
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("unexpected updates order/content: %+v", got)
	}
}

func TestDispatchLoopExitsOnContextDoneWhileSending(t *testing.T) {
	queue := make(chan AuthUpdate) // unbuffered to block sends
	w := &Watcher{
		authQueue: queue,
		pendingUpdates: map[string]AuthUpdate{
			"k": {Action: AuthUpdateActionAdd, ID: "k"}},
		pendingOrder: []string{"k"}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.dispatchLoop(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected dispatchLoop to exit after ctx canceled while blocked on send")
	}
}

func TestProcessEventsHandlesEventErrorAndChannelClose(t *testing.T) {
	w := &Watcher{
		watcher: &fsnotify.Watcher{
			Events: make(chan fsnotify.Event, 2),
			Errors: make(chan error, 2)},
		configPath: "config.yaml",
		authDir:    "auth"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.processEvents(ctx)
		close(done)
	}()

	w.watcher.Events <- fsnotify.Event{Name: "unrelated.txt", Op: fsnotify.Write}
	w.watcher.Errors <- fmt.Errorf("watcher error")

	time.Sleep(20 * time.Millisecond)
	close(w.watcher.Events)
	close(w.watcher.Errors)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processEvents did not exit after channels closed")
	}
}

func TestProcessEventsReturnsWhenErrorsChannelClosed(t *testing.T) {
	w := &Watcher{
		watcher: &fsnotify.Watcher{
			Events: nil,
			Errors: make(chan error)}}

	close(w.watcher.Errors)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.processEvents(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processEvents did not exit after errors channel closed")
	}
}

func TestHandleEventIgnoresUnrelatedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("auth_dir: "+authDir+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	var reloads int32
	w := &Watcher{
		authDir:        authDir,
		configPath:     configPath,
		lastAuthHashes: make(map[string]string),
		reloadCallback: func(*config.Config) { atomic.AddInt32(&reloads, 1) }}
	w.SetConfig(&config.Config{})

	w.handleEvent(fsnotify.Event{Name: filepath.Join(tmpDir, "note.txt"), Op: fsnotify.Write})
	if atomic.LoadInt32(&reloads) != 0 {
		t.Fatalf("expected no reloads for unrelated file, got %d", reloads)
	}
}

func TestHandleEventConfigChangeSchedulesReload(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("auth_dir: "+authDir+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	var reloads int32
	w := &Watcher{
		authDir:        authDir,
		configPath:     configPath,
		lastAuthHashes: make(map[string]string),
		reloadCallback: func(*config.Config) { atomic.AddInt32(&reloads, 1) }}
	w.SetConfig(&config.Config{})

	w.handleEvent(fsnotify.Event{Name: configPath, Op: fsnotify.Write})

	time.Sleep(400 * time.Millisecond)
	if atomic.LoadInt32(&reloads) != 1 {
		t.Fatalf("expected config change to trigger reload once, got %d", reloads)
	}
}

func TestRefreshAuthStateDispatchesRuntimeAuths(t *testing.T) {
	queue := make(chan AuthUpdate, 8)
	w := &Watcher{
		authDir:        t.TempDir(),
		lastAuthHashes: make(map[string]string)}
	w.SetConfig(&config.Config{})
	w.SetAuthUpdateQueue(queue)
	defer w.stopDispatch()

	w.clientsMutex.Lock()
	w.runtimeAuths = map[string]*coreauth.Auth{
		"nil": nil,
		"r1":  {ID: "r1", Provider: "runtime"}}
	w.clientsMutex.Unlock()

	w.refreshAuthState(false)

	select {
	case u := <-queue:
		if u.Action != AuthUpdateActionAdd || u.ID != "r1" {
			t.Fatalf("unexpected auth update: %+v", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime auth update")
	}
}

func TestLoadFileClientsWalkError(t *testing.T) {
	tmpDir := t.TempDir()
	noAccessDir := filepath.Join(tmpDir, "0noaccess")
	if err := os.MkdirAll(noAccessDir, 0o755); err != nil {
		t.Fatalf("failed to create noaccess dir: %v", err)
	}
	if err := os.Chmod(noAccessDir, 0); err != nil {
		t.Skipf("chmod not supported: %v", err)
	}
	defer func() { _ = os.Chmod(noAccessDir, 0o755) }()

	cfg := &config.Config{}
	w := &Watcher{}
	w.SetConfig(cfg)

	count := w.loadFileClients(cfg)
	if count != 0 {
		t.Fatalf("expected count 0 due to walk error, got %d", count)
	}
}

func TestReloadConfigIfChangedHandlesMissingAndEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	w := &Watcher{
		configPath: filepath.Join(tmpDir, "missing.yaml"),
		authDir:    authDir}
	w.reloadConfigIfChanged() // missing file -> log + return

	emptyPath := filepath.Join(tmpDir, "empty.yaml")
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write empty config: %v", err)
	}
	w.configPath = emptyPath
	w.reloadConfigIfChanged() // empty file -> early return
}

func TestReloadConfigSucceedsWithoutAuthDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	w := &Watcher{
		configPath:     configPath,
		lastAuthHashes: make(map[string]string),
	}
	w.SetConfig(&config.Config{})

	if ok := w.reloadConfig(); !ok {
		t.Fatal("expected reloadConfig to succeed")
	}

	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()
	if w.config == nil || w.config.Port != 8317 {
		t.Fatalf("expected reloaded port 8317, got %+v", w.config)
	}
}

func TestReloadConfigTriggersCallbackForMaxRetryCredentialsChange(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldCfg := &config.Config{
		CredentialInFlight:  config.DefaultCredentialInFlightConfig(),
		MaxRetryCredentials: 0,
		RequestRetry:        1,
		MaxRetryInterval:    5}
	newCfg := &config.Config{
		CredentialInFlight:  config.DefaultCredentialInFlightConfig(),
		MaxRetryCredentials: 2,
		RequestRetry:        1,
		MaxRetryInterval:    5}
	data, errMarshal := yaml.Marshal(newCfg)
	if errMarshal != nil {
		t.Fatalf("failed to marshal config: %v", errMarshal)
	}
	if errWrite := os.WriteFile(configPath, data, 0o644); errWrite != nil {
		t.Fatalf("failed to write config: %v", errWrite)
	}

	callbackCalls := 0
	callbackMaxRetryCredentials := -1
	w := &Watcher{
		configPath:     configPath,
		authDir:        authDir,
		lastAuthHashes: make(map[string]string),
		reloadCallback: func(cfg *config.Config) {
			callbackCalls++
			if cfg != nil {
				callbackMaxRetryCredentials = cfg.MaxRetryCredentials
			}
		}}
	w.SetConfig(oldCfg)

	if ok := w.reloadConfig(); !ok {
		t.Fatal("expected reloadConfig to succeed")
	}

	if callbackCalls != 1 {
		t.Fatalf("expected reload callback to be called once, got %d", callbackCalls)
	}
	if callbackMaxRetryCredentials != 2 {
		t.Fatalf("expected callback MaxRetryCredentials=2, got %d", callbackMaxRetryCredentials)
	}

	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()
	if w.config == nil || w.config.MaxRetryCredentials != 2 {
		t.Fatalf("expected watcher config MaxRetryCredentials=2, got %+v", w.config)
	}
}

func TestStartSucceedsWhenAuthDirMissing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("auth_dir: "+filepath.Join(tmpDir, "missing-auth")+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	authDir := filepath.Join(tmpDir, "missing-auth")

	w, err := NewWatcher(configPath, authDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Stop()
	w.SetConfig(&config.Config{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v, want success without auth-dir", err)
	}
}

func TestDispatchRuntimeAuthUpdateReturnsFalseWithoutQueue(t *testing.T) {
	w := &Watcher{}
	if ok := w.DispatchRuntimeAuthUpdate(AuthUpdate{Action: AuthUpdateActionAdd, Auth: &coreauth.Auth{ID: "a"}}); ok {
		t.Fatal("expected DispatchRuntimeAuthUpdate to return false when no queue configured")
	}
	if ok := w.DispatchRuntimeAuthUpdate(AuthUpdate{Action: AuthUpdateActionDelete, Auth: &coreauth.Auth{ID: "a"}}); ok {
		t.Fatal("expected DispatchRuntimeAuthUpdate delete to return false when no queue configured")
	}
}

func TestNormalizeAuthNil(t *testing.T) {
	if normalizeAuth(nil) != nil {
		t.Fatal("expected normalizeAuth(nil) to return nil")
	}
}

// stubStore implements coreauth.Store plus watcher-specific persistence helpers.
type stubStore struct {
	mu              sync.Mutex
	authDir         string
	cfgPersisted    int
	authPersisted   int
	lastAuthMessage string
	lastAuthPaths   []string
	persisted       chan struct{}
}

func (s *stubStore) List(context.Context) ([]*coreauth.Auth, error) { return nil, nil }
func (s *stubStore) Save(context.Context, *coreauth.Auth) (string, error) {
	return "", nil
}
func (s *stubStore) Delete(context.Context, string) error { return nil }
func (s *stubStore) PersistConfig(context.Context) error {
	s.mu.Lock()
	s.cfgPersisted++
	s.mu.Unlock()
	s.signalPersisted()
	return nil
}
func (s *stubStore) PersistAuthFiles(_ context.Context, message string, paths ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAuthMessage = message
	s.lastAuthPaths = append([]string(nil), paths...)
	s.authPersisted++
	s.signalPersisted()
	return nil
}
func (s *stubStore) signalPersisted() {
	if s.persisted == nil {
		return
	}
	select {
	case s.persisted <- struct{}{}:
	default:
	}
}

func (s *stubStore) persistenceSnapshot() (cfgPersisted, authPersisted int, message string, paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfgPersisted, s.authPersisted, s.lastAuthMessage, append([]string(nil), s.lastAuthPaths...)
}

func TestPersistConfigAndAuthAsyncInvokePersister(t *testing.T) {
	store := &stubStore{persisted: make(chan struct{}, 2)}
	w := &Watcher{
		storePersister: store}

	w.persistConfigAsync()

	select {
	case <-store.persisted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for asynchronous persistence")
	}
	cfgPersisted, authPersisted, _, _ := store.persistenceSnapshot()
	if cfgPersisted != 1 {
		t.Fatalf("expected PersistConfig to be called once, got %d", cfgPersisted)
	}
	if authPersisted != 0 {
		t.Fatalf("expected PersistAuthFiles not to be called, got %d", authPersisted)
	}
}

func TestScheduleConfigReloadDebounces(t *testing.T) {
	tmp := t.TempDir()
	authDir := tmp
	cfgPath := tmp + "/config.yaml"
	if err := os.WriteFile(cfgPath, []byte("auth_dir: "+authDir+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var reloads int32
	w := &Watcher{
		configPath:     cfgPath,
		authDir:        authDir,
		reloadCallback: func(*config.Config) { atomic.AddInt32(&reloads, 1) }}
	w.SetConfig(&config.Config{})

	w.scheduleConfigReload()
	w.scheduleConfigReload()

	deadline := time.Now().Add(time.Second)
	for {
		w.clientsMutex.RLock()
		hashSet := w.lastConfigHash != ""
		w.clientsMutex.RUnlock()
		if hashSet {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for debounced config reload")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&reloads); got != 1 {
		t.Fatalf("expected single debounced reload, got %d", got)
	}
}

func TestPrepareAuthUpdatesLockedForceAndDelete(t *testing.T) {
	w := &Watcher{
		currentAuths: map[string]*coreauth.Auth{
			"a": {ID: "a", Provider: "p1"}},
		authQueue: make(chan AuthUpdate, 4)}

	updates := w.prepareAuthUpdatesLocked([]*coreauth.Auth{{ID: "a", Provider: "p2"}}, false)
	if len(updates) != 1 || updates[0].Action != AuthUpdateActionModify || updates[0].ID != "a" {
		t.Fatalf("unexpected modify updates: %+v", updates)
	}

	updates = w.prepareAuthUpdatesLocked([]*coreauth.Auth{{ID: "a", Provider: "p2"}}, true)
	if len(updates) != 1 || updates[0].Action != AuthUpdateActionModify {
		t.Fatalf("expected force modify, got %+v", updates)
	}

	updates = w.prepareAuthUpdatesLocked([]*coreauth.Auth{}, false)
	if len(updates) != 1 || updates[0].Action != AuthUpdateActionDelete || updates[0].ID != "a" {
		t.Fatalf("expected delete for missing auth, got %+v", updates)
	}
}

func TestAuthEqualIgnoresTemporalFields(t *testing.T) {
	now := time.Now()
	a := &coreauth.Auth{ID: "x", CreatedAt: now}
	b := &coreauth.Auth{ID: "x", CreatedAt: now.Add(5 * time.Second)}
	if !authEqual(a, b) {
		t.Fatal("expected authEqual to ignore temporal differences")
	}
}

func TestDispatchLoopExitsWhenQueueNilAndContextCanceled(t *testing.T) {
	w := &Watcher{
		dispatchCond:   nil,
		pendingUpdates: map[string]AuthUpdate{"k": {ID: "k"}},
		pendingOrder:   []string{"k"}}
	w.dispatchCond = sync.NewCond(&w.dispatchMu)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.dispatchLoop(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	w.dispatchMu.Lock()
	w.dispatchCond.Broadcast()
	w.dispatchMu.Unlock()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dispatchLoop did not exit after context cancel")
	}
}

func TestScheduleProcessEventsStopsOnContextDone(t *testing.T) {
	w := &Watcher{
		watcher: &fsnotify.Watcher{
			Events: make(chan fsnotify.Event, 1),
			Errors: make(chan error, 1)},
		configPath: "config.yaml",
		authDir:    "auth"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.processEvents(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("processEvents did not exit on context cancel")
	}
}

func hexString(data []byte) string {
	return strings.ToLower(fmt.Sprintf("%x", data))
}
