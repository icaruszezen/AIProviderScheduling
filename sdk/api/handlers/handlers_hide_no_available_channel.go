package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
)

// DownstreamErrorStatusAndBody returns the HTTP status and JSON body that should
// be written to a downstream client, applying the no-available-channel rewrite.
func DownstreamErrorStatusAndBody(msg *interfaces.ErrorMessage) (int, []byte) {
	if ShouldHideNoAvailableChannel(msg) {
		msg = SanitizeHiddenNoAvailableChannelMessage(msg)
	}
	status := http.StatusInternalServerError
	if msg != nil && msg.StatusCode > 0 {
		status = msg.StatusCode
	}
	errText := http.StatusText(status)
	if msg != nil && msg.Error != nil {
		if v := strings.TrimSpace(msg.Error.Error()); v != "" {
			errText = v
		}
	}
	return status, BuildErrorResponseBody(status, errText)
}

// ShouldHideNoAvailableChannel reports that the downstream payload should be
// replaced with a generic 503 Service Unavailable envelope.
func ShouldHideNoAvailableChannel(msg *interfaces.ErrorMessage) bool {
	return msg != nil && msg.HideNoAvailableChannel
}

// SanitizeHiddenNoAvailableChannelMessage returns a generic 503 message for
// downstream clients. The original msg is left unchanged.
func SanitizeHiddenNoAvailableChannelMessage(msg *interfaces.ErrorMessage) *interfaces.ErrorMessage {
	if !ShouldHideNoAvailableChannel(msg) {
		return msg
	}
	return &interfaces.ErrorMessage{
		StatusCode:             http.StatusServiceUnavailable,
		Error:                  errors.New(http.StatusText(http.StatusServiceUnavailable)),
		HideNoAvailableChannel: true,
	}
}

func originalErrorLogBody(msg *interfaces.ErrorMessage) []byte {
	if msg == nil {
		return nil
	}
	if msg.DirectResponse && len(msg.Body) > 0 {
		return msg.Body
	}
	status := http.StatusInternalServerError
	if msg.StatusCode > 0 {
		status = msg.StatusCode
	}
	errText := http.StatusText(status)
	if msg.Error != nil {
		if v := strings.TrimSpace(msg.Error.Error()); v != "" {
			errText = v
		}
	}
	return BuildErrorResponseBody(status, errText)
}

// SnapshotAPIResponse returns the current request-log API_RESPONSE value.
func SnapshotAPIResponse(c *gin.Context) (any, bool) {
	if c == nil {
		return nil, false
	}
	return c.Get("API_RESPONSE")
}

// RestoreAPIResponse puts the captured request-log payload back after a rewritten write.
func RestoreAPIResponse(c *gin.Context, previous any, existed bool) {
	if c == nil {
		return
	}
	if existed {
		c.Set("API_RESPONSE", previous)
	}
}

// PreserveOriginalErrorLog records the unsanitized error body for request logs.
func PreserveOriginalErrorLog(c *gin.Context, msg *interfaces.ErrorMessage) {
	if c == nil {
		return
	}
	appendAPIResponse(c, originalErrorLogBody(msg))
}
