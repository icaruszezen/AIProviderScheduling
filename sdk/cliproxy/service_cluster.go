package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/cluster"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func (s *Service) ensureClusterService() {
	if s == nil || s.clusterService != nil {
		return
	}
	s.clusterService = cluster.NewService(
		func() *config.Config {
			if s == nil {
				return nil
			}
			s.cfgMu.RLock()
			defer s.cfgMu.RUnlock()
			return s.cfg
		},
		func() string {
			if s == nil {
				return ""
			}
			return s.configPath
		},
		func(ctx context.Context, cfg *config.Config) error {
			s.applyConfigUpdate(cfg)
			return nil
		},
	)
}
