package executor

import (
	"context"
	"net/http"

	"github.com/icaruszezen/AIProviderScheduling/v7/internal/runtime/executor/helps"
	apsauth "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/auth"
	log "github.com/sirupsen/logrus"
)

// Refresh replaces credentials via Home when enabled. Account OAuth token
// refresh is not supported; API keys are returned unchanged.
func (e *XAIExecutor) Refresh(ctx context.Context, auth *apsauth.Auth) (*apsauth.Auth, error) {
	log.Debugf("xai executor: refresh called")
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	if auth == nil {
		return nil, statusErr{code: http.StatusInternalServerError, msg: "xai executor: auth is nil"}
	}
	return auth, nil
}
