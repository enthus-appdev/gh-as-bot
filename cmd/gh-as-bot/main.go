// Command gh-as-bot is a gh CLI extension that runs gh authenticated as a
// GitHub App installation. It mints a fresh installation access token from
// the configured App credentials, sets GH_TOKEN for one invocation, and
// execs gh with the caller's arguments — leaving the user's persistent
// `gh auth` state untouched.
package main

import (
	"os"

	"github.com/enthus-appdev/gh-as-bot/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
