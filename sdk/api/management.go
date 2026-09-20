// Package api exposes helpers for embedding CLIProxyAPI.
//
// It wraps internal management handler types and helpers so external projects
// can integrate management endpoints without importing internal packages.
package api

import (
	internalmanagement "github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

// Handler re-exports the management handler used by the internal HTTP API.
type Handler = internalmanagement.Handler

// NewHandler creates a management handler for SDK consumers.
func NewHandler(cfg *config.Config, configFilePath string, manager *coreauth.Manager) *Handler {
	return internalmanagement.NewHandler(cfg, configFilePath, manager)
}

// NewHandlerWithoutConfigFilePath creates a management handler that skips config file persistence.
func NewHandlerWithoutConfigFilePath(cfg *config.Config, manager *coreauth.Manager) *Handler {
	return internalmanagement.NewHandlerWithoutConfigFilePath(cfg, manager)
}

// WriteConfig persists management configuration to disk.
func WriteConfig(path string, data []byte) error {
	return internalmanagement.WriteConfig(path, data)
}
