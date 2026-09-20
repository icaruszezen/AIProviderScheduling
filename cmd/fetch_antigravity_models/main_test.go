package main

import (
	"strings"
	"testing"
)

func TestDocumentsRetiredCatalogFetch(t *testing.T) {
	if !strings.Contains(oauthAuthDirUnsupportedMessage, "retired") {
		t.Fatalf("message = %q, want to document that the catalog fetcher is retired", oauthAuthDirUnsupportedMessage)
	}
	if !strings.Contains(oauthAuthDirUnsupportedMessage, "antigravity-api-key") {
		t.Fatalf("message = %q, want to point operators at antigravity-api-key", oauthAuthDirUnsupportedMessage)
	}
}
