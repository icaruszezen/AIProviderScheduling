package helps

import (
	"reflect"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestStreamFakeFirstTokensFromAuth(t *testing.T) {
	if got := StreamFakeFirstTokensFromAuth(nil); got != nil {
		t.Fatalf("nil auth = %#v, want nil", got)
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		MetadataStreamFakeFirstTokens: []any{" ", "-", "", " "}}}
	got := StreamFakeFirstTokensFromAuth(auth)
	want := []string{" ", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

func TestIsStreamFakeFirstToken(t *testing.T) {
	tokens := []string{" ", "-"}
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "space delta", payload: `{"type":"response.output_text.delta","delta":" "}`, want: true},
		{name: "dash delta", payload: `{"type":"response.reasoning_text.delta","delta":"-"}`, want: true},
		{name: "real text", payload: `{"type":"response.output_text.delta","delta":"hello"}`, want: false},
		{name: "prefix space", payload: `{"type":"response.output_text.delta","delta":" hello"}`, want: false},
		{name: "sse space", payload: `data: {"type":"response.output_text.delta","delta":" "}`, want: true},
		{name: "done space", payload: `{"type":"response.output_text.done","text":" "}`, want: true},
		{name: "tool delta", payload: `{"type":"response.function_call_arguments.delta","delta":"-"}`, want: false},
		{name: "handshake", payload: `{"type":"response.created"}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsStreamFakeFirstToken([]byte(tt.payload), tokens); got != tt.want {
				t.Fatalf("IsStreamFakeFirstToken(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsResponsesHoldableLifecycle(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "created", payload: `{"type":"response.created"}`, want: true},
		{name: "output item added", payload: `{"type":"response.output_item.added"}`, want: true},
		{name: "content part added", payload: `{"type":"response.content_part.added"}`, want: true},
		{name: "text delta", payload: `{"type":"response.output_text.delta","delta":"hi"}`, want: false},
		{name: "error", payload: `{"type":"error"}`, want: false},
		{name: "completed", payload: `{"type":"response.completed"}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsResponsesHoldableLifecycle([]byte(tt.payload)); got != tt.want {
				t.Fatalf("IsResponsesHoldableLifecycle(%s) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
