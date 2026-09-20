// Command fetch_antigravity_models previously fetched model catalogs using
// OAuth auth-dir credentials. Account OAuth is no longer supported.
package main

import (
	"fmt"
	"os"
)

const oauthAuthDirUnsupportedMessage = "fetch_antigravity_models: retired; account OAuth catalog fetch is gone. Configure antigravity-api-key (with project-id) in config.yaml"

func main() {
	fmt.Fprintln(os.Stderr, oauthAuthDirUnsupportedMessage)
	os.Exit(1)
}
