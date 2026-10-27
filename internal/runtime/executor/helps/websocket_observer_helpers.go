package helps

import (
	"context"

	apsauth "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/auth"
	apsexecutor "github.com/icaruszezen/AIProviderScheduling/v7/sdk/aps/executor"
	"github.com/tidwall/gjson"
)

// EmitWebSocketResponseEvent delivers an upstream WebSocket response frame to any configured observer.
func EmitWebSocketResponseEvent(ctx context.Context, opts apsexecutor.Options, auth *apsauth.Auth, provider, model string, payload []byte) {
	if opts.WebSocketResponseObserver == nil || len(payload) == 0 {
		return
	}
	var authID, authLabel, authType string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, _ = auth.AccountInfo()
	}
	eventType := gjson.GetBytes(payload, "type").String()
	var requestID string
	var traceID string
	if opts.Metadata != nil {
		if reqID, ok := opts.Metadata["request_id"].(string); ok {
			requestID = reqID
		}
		if trID, ok := opts.Metadata["trace_id"].(string); ok {
			traceID = trID
		}
	}
	requestedModel := PayloadRequestedModel(opts, model)
	opts.WebSocketResponseObserver(ctx, apsexecutor.WebSocketResponseEvent{
		RequestID:      requestID,
		TraceID:        traceID,
		SourceFormat:   opts.SourceFormat.String(),
		Model:          model,
		RequestedModel: requestedModel,
		Provider:       provider,
		AuthID:         authID,
		AuthLabel:      authLabel,
		AuthType:       authType,
		EventType:      eventType,
		Payload:        payload,
		Metadata:       opts.Metadata})
}
