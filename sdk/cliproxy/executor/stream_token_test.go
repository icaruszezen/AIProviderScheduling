package executor

import (
	"testing"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestIsStreamTokenPayloadInteractions(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{
			name:    "created",
			payload: `{"event_type":"interaction.created","interaction":{"id":"i1"}}`,
			want:    false,
		},
		{
			name:    "step start",
			payload: `{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
			want:    false,
		},
		{
			name:    "step stop",
			payload: `{"event_type":"step.stop","index":0}`,
			want:    false,
		},
		{
			name:    "text delta",
			payload: `{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hi"}}`,
			want:    true,
		},
		{
			name:    "sse text delta",
			payload: "event: step.delta\ndata: {\"event_type\":\"step.delta\",\"delta\":{\"type\":\"text\",\"text\":\"hi\"}}\n\n",
			want:    true,
		},
		{
			name:    "thought summary",
			payload: `{"event_type":"step.delta","delta":{"type":"thought_summary","content":{"type":"text","text":"thinking"}}}`,
			want:    true,
		},
		{
			name:    "empty arguments",
			payload: `{"event_type":"step.delta","delta":{"type":"arguments_delta","arguments":""}}`,
			want:    false,
		},
		{
			name:    "tool arguments",
			payload: `{"event_type":"step.delta","delta":{"type":"arguments_delta","arguments":"{\"q\":1}"}}`,
			want:    true,
		},
		{
			name:    "completed",
			payload: `{"event_type":"interaction.completed","interaction":{"status":"completed"}}`,
			want:    false,
		},
		{
			name:    "error",
			payload: `{"event_type":"error","error":{"message":"overloaded"}}`,
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStreamTokenPayload(sdktranslator.FormatInteractions, []byte(tt.payload)); got != tt.want {
				t.Fatalf("IsStreamTokenPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsStreamTokenPayloadIgnoresTerminalMarkers(t *testing.T) {
	tests := []struct {
		name    string
		format  sdktranslator.Format
		payload string
		want    bool
	}{
		{name: "chat done", format: sdktranslator.FormatOpenAI, payload: "data: [DONE]", want: false},
		{name: "chat finish only", format: sdktranslator.FormatOpenAI, payload: `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, want: false},
		{name: "chat text", format: sdktranslator.FormatOpenAI, payload: `{"choices":[{"delta":{"content":"hi"}}]}`, want: true},
		{name: "claude stop", format: sdktranslator.FormatClaude, payload: `{"type":"message_stop"}`, want: false},
		{name: "claude stop reason", format: sdktranslator.FormatClaude, payload: `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, want: false},
		{name: "claude text", format: sdktranslator.FormatClaude, payload: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`, want: true},
		{name: "claude tool start", format: sdktranslator.FormatClaude, payload: `{"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_1","name":"lookup"}}`, want: true},
		{name: "responses completed", format: sdktranslator.FormatOpenAIResponse, payload: `{"type":"response.completed","response":{"status":"completed"}}`, want: false},
		{name: "responses text", format: sdktranslator.FormatCodex, payload: `{"type":"response.output_text.delta","delta":"hi"}`, want: true},
		{name: "gemini finish only", format: sdktranslator.FormatGemini, payload: `{"candidates":[{"finishReason":"STOP"}]}`, want: false},
		{name: "gemini text", format: sdktranslator.FormatGemini, payload: `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStreamTokenPayload(tt.format, []byte(tt.payload)); got != tt.want {
				t.Fatalf("IsStreamTokenPayload() = %v, want %v", got, tt.want)
			}
		})
	}
	if !IsResponsesTokenEvent([]byte(`{"type":"response.completed","response":{"status":"completed"}}`)) {
		t.Fatal("TTFT classifier must still treat response.completed as a token event")
	}
	if !IsChatTokenEvent([]byte("data: [DONE]")) {
		t.Fatal("TTFT classifier must still treat [DONE] as a token event")
	}
}
