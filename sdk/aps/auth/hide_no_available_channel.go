package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	hideNoAvailableChannelMetadataKey       = "hide_no_available_channel"
	hideNoAvailableChannelMetadataKeyLegacy = "hide-no-available-channel"
	noAvailableChannelPhrase                = "no available channel for model"
)

type hideNoAvailableChannelError struct {
	error
}

func (e hideNoAvailableChannelError) Unwrap() error { return e.error }

func (e hideNoAvailableChannelError) HideNoAvailableChannel() bool { return true }

func (e hideNoAvailableChannelError) StatusCode() int {
	return statusCodeFromError(e.error)
}

func (e hideNoAvailableChannelError) RetryAfter() *time.Duration {
	type retryAfterProvider interface {
		RetryAfter() *time.Duration
	}
	var rap retryAfterProvider
	if errors.As(e.error, &rap) && rap != nil {
		return rap.RetryAfter()
	}
	return nil
}

func authHidesNoAvailableChannel(auth *Auth) bool {
	if auth == nil || len(auth.Metadata) == 0 {
		return false
	}
	return metadataTruthy(auth.Metadata[hideNoAvailableChannelMetadataKey]) ||
		metadataTruthy(auth.Metadata[hideNoAvailableChannelMetadataKeyLegacy])
}

func metadataTruthy(raw any) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func containsNoAvailableChannel(text string) bool {
	return strings.Contains(strings.ToLower(text), noAvailableChannelPhrase)
}

// IsNoAvailableChannelError reports a 503 whose body mentions
// "No available channel for model".
func IsNoAvailableChannelError(err error) bool {
	if err == nil {
		return false
	}
	if statusCodeFromError(err) != http.StatusServiceUnavailable {
		return false
	}
	return containsNoAvailableChannel(extractErrorBody(err))
}

// HidesNoAvailableChannel reports whether err was marked for downstream
// replacement of a "No available channel for model" 503.
func HidesNoAvailableChannel(err error) bool {
	if err == nil {
		return false
	}
	type marker interface {
		HideNoAvailableChannel() bool
	}
	var marked marker
	return errors.As(err, &marked) && marked != nil && marked.HideNoAvailableChannel()
}

// MarkHideNoAvailableChannel wraps err so downstream writers can replace the
// "No available channel for model" payload without changing Error().
func MarkHideNoAvailableChannel(err error) error {
	if err == nil || HidesNoAvailableChannel(err) {
		return err
	}
	return hideNoAvailableChannelError{error: err}
}

func markHideNoAvailableChannel(err error) error {
	return MarkHideNoAvailableChannel(err)
}

func maybeMarkHideNoAvailableChannel(auth *Auth, err error) error {
	if err == nil || !authHidesNoAvailableChannel(auth) || !IsNoAvailableChannelError(err) {
		return err
	}
	return markHideNoAvailableChannel(err)
}
