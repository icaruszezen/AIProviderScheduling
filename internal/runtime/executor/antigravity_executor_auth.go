package executor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// Refresh replaces credentials via Home when enabled. Account OAuth token
// refresh is not supported; provider API keys are returned unchanged.
func (e *AntigravityExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	return auth, nil
}

func (e *AntigravityExecutor) ShouldPrepareRequestAuth(auth *cliproxyauth.Auth) bool {
	return antigravityProjectIDFromAuth(auth) == ""
}

func (e *AntigravityExecutor) PrepareRequestAuth(_ context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil || !e.ShouldPrepareRequestAuth(auth) {
		return nil, nil
	}
	return nil, missingAntigravityProjectIDError(nil)
}

func (e *AntigravityExecutor) ensureAccessToken(ctx context.Context, auth *cliproxyauth.Auth) (string, *cliproxyauth.Auth, error) {
	if auth == nil {
		return "", nil, statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
	}
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		if err != nil {
			return "", nil, err
		}
		token := antigravityAPIKey(refreshed)
		if token == "" {
			return "", nil, statusErr{code: http.StatusUnauthorized, msg: "missing api key"}
		}
		e.maybeRefreshAntigravityCreditsHint(ctx, refreshed, token)
		return token, refreshed, nil
	}
	token := antigravityAPIKey(auth)
	if token == "" {
		return "", nil, statusErr{code: http.StatusUnauthorized, msg: "missing api key"}
	}
	e.maybeRefreshAntigravityCreditsHint(ctx, auth, token)
	return token, nil, nil
}

func antigravityAPIKey(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes["api_key"])
}

func (e *AntigravityExecutor) projectIDForRequest(_ context.Context, auth *cliproxyauth.Auth, _ string) (string, error) {
	if projectID := antigravityProjectIDFromAuth(auth); projectID != "" {
		return projectID, nil
	}
	return "", missingAntigravityProjectIDError(nil)
}

func antigravityProjectIDFromAuth(auth *cliproxyauth.Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if pid, ok := auth.Metadata["project_id"].(string); ok {
		return strings.TrimSpace(pid)
	}
	return ""
}

func missingAntigravityProjectIDError(cause error) statusErr {
	msg := "antigravity auth missing project_id"
	statusCode := http.StatusBadRequest
	var retryAfter *time.Duration
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, cause)
		type statusCoder interface {
			StatusCode() int
		}
		var sc statusCoder
		if errors.As(cause, &sc) && sc != nil {
			if code := sc.StatusCode(); code > 0 {
				statusCode = code
			}
		}
		type retryAfterProvider interface {
			RetryAfter() *time.Duration
		}
		var rap retryAfterProvider
		if errors.As(cause, &rap) && rap != nil {
			retryAfter = rap.RetryAfter()
		}
	}
	return statusErr{code: statusCode, msg: msg, retryAfter: retryAfter}
}

func metaStringValue(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata[key]; ok {
		switch typed := v.(type) {
		case string:
			return strings.TrimSpace(typed)
		case []byte:
			return strings.TrimSpace(string(typed))
		}
	}
	return ""
}
