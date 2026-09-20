package cliproxy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type serviceCooldownStateStore struct{}

func (*serviceCooldownStateStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (*serviceCooldownStateStore) Save(context.Context, []coreauth.CooldownStateRecord) error {
	return nil
}

func TestResolveCooldownStateStoreUsesExplicitOverride(t *testing.T) {
	providedStore := &serviceCooldownStateStore{}
	cfg := &config.Config{}
	service, errBuild := NewBuilder().
		WithConfig(cfg).
		WithConfigPath(filepath.Join(t.TempDir(), "config.yaml")).
		WithCooldownStateStore(providedStore).
		Build()
	if errBuild != nil {
		t.Fatalf("Build() error = %v", errBuild)
	}

	got := service.resolveCooldownStateStore(cfg)
	if got != providedStore {
		t.Fatalf("resolveCooldownStateStore() = %T, want explicit override", got)
	}
}
