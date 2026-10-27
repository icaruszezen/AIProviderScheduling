package logging

import (
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// APSTraceIDHeader is the downstream response header used to correlate requests with selected credentials.
const APSTraceIDHeader = "X-APS-TRACE-ID"

const ginAPSTraceStateKey = "__aps_trace_state__"

// FormatAPSTraceID builds a APS trace ID from the selection time, auth index, and request ID.
func FormatAPSTraceID(selectedAt time.Time, authIndex, requestID string) string {
	authIndex = strings.TrimSpace(authIndex)
	requestID = strings.TrimSpace(requestID)
	if selectedAt.IsZero() || authIndex == "" || requestID == "" {
		return ""
	}
	return selectedAt.Format("20060102150405") + "-" + authIndex + "-" + requestID
}

type apsTraceState struct {
	mu      sync.RWMutex
	traceID string
}

func (s *apsTraceState) set(traceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.traceID = strings.TrimSpace(traceID)
	s.mu.Unlock()
}

func (s *apsTraceState) get() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	traceID := s.traceID
	s.mu.RUnlock()
	return traceID
}

func ginAPSTraceState(c *gin.Context) *apsTraceState {
	if c == nil {
		return nil
	}
	if value, exists := c.Get(ginAPSTraceStateKey); exists {
		if state, ok := value.(*apsTraceState); ok && state != nil {
			return state
		}
	}
	state := &apsTraceState{}
	c.Set(ginAPSTraceStateKey, state)
	return state
}

// GinAPSTraceIDCallback returns a callback that is safe to invoke after the Gin context is released.
func GinAPSTraceIDCallback(c *gin.Context) func(string) {
	state := ginAPSTraceState(c)
	if state == nil {
		return nil
	}
	requestID := GetGinRequestID(c)
	if requestID == "" && c.Request != nil {
		requestID = GetRequestID(c.Request.Context())
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	return func(authIndex string) {
		if traceID := FormatAPSTraceID(time.Now(), authIndex, requestID); traceID != "" {
			state.set(traceID)
		}
	}
}

// SetGinAPSTraceID stores the trace ID until the downstream response headers are committed.
func SetGinAPSTraceID(c *gin.Context, authIndex string) {
	if callback := GinAPSTraceIDCallback(c); callback != nil {
		callback(authIndex)
	}
}

// GetGinAPSTraceID returns the trace ID stored for the current request.
func GetGinAPSTraceID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, exists := c.Get(ginAPSTraceStateKey)
	if !exists {
		return ""
	}
	state, _ := value.(*apsTraceState)
	return state.get()
}

// APSTraceIDMiddleware injects a stored trace ID immediately before response headers are committed.
func APSTraceIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		state := ginAPSTraceState(c)
		c.Writer = &apsTraceResponseWriter{ResponseWriter: c.Writer, state: state}
		c.Next()
	}
}

type apsTraceResponseWriter struct {
	gin.ResponseWriter
	state *apsTraceState
}

func (w *apsTraceResponseWriter) WriteHeader(statusCode int) {
	w.applyTraceHeader()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *apsTraceResponseWriter) WriteHeaderNow() {
	w.applyTraceHeader()
	w.ResponseWriter.WriteHeaderNow()
}

func (w *apsTraceResponseWriter) Write(data []byte) (int, error) {
	w.applyTraceHeader()
	return w.ResponseWriter.Write(data)
}

func (w *apsTraceResponseWriter) WriteString(data string) (int, error) {
	w.applyTraceHeader()
	return w.ResponseWriter.WriteString(data)
}

func (w *apsTraceResponseWriter) Flush() {
	w.applyTraceHeader()
	w.ResponseWriter.Flush()
}

func (w *apsTraceResponseWriter) applyTraceHeader() {
	if w == nil || w.ResponseWriter == nil || w.ResponseWriter.Written() {
		return
	}
	if traceID := w.state.get(); traceID != "" {
		w.ResponseWriter.Header().Set(APSTraceIDHeader, traceID)
	}
}
