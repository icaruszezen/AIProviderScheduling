package openai

import (
	"bytes"
	"testing"
)

func TestWriteSSEDataMatchesLegacyFrame(t *testing.T) {
	var buf bytes.Buffer
	writeSSEData(&buf, []byte(`{"a":1}`))
	if got := buf.String(); got != "data: {\"a\":1}\n\n" {
		t.Fatalf("chunk frame = %q", got)
	}

	buf.Reset()
	writeSSEData(&buf, sseDoneToken)
	if got := buf.String(); got != "data: [DONE]\n\n" {
		t.Fatalf("done frame = %q", got)
	}

	buf.Reset()
	writeSSEData(&buf, nil)
	if got := buf.String(); got != "data: \n\n" {
		t.Fatalf("empty frame = %q", got)
	}
}
