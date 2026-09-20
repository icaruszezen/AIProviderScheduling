// Command fetch_codex_models previously fetched Codex model catalogs using
// OAuth auth-dir credentials. Account OAuth is no longer supported.
package main

import (
	"fmt"
	"os"
)

const oauthAuthDirUnsupportedMessage = "fetch_codex_models: retired; account OAuth catalog fetch is gone. Configure models on codex-api-key in config.yaml"

func main() {
	fmt.Fprintln(os.Stderr, oauthAuthDirUnsupportedMessage)
	os.Exit(1)
}
