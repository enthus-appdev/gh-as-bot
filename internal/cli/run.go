package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/enthus-appdev/gh-as-bot/internal/app"
)

const usage = `gh as-bot — run gh authenticated as a GitHub App installation.

Usage:
  gh as-bot setup            Walk through GitHub App configuration (start here)
  gh as-bot <gh args...>     Run gh with bot credentials
  gh as-bot --token          Print a fresh installation token to stdout
  gh as-bot doctor           Verify config and credentials
  gh as-bot help             Show this message

Required env:
  GH_AS_BOT_APP_ID            Numeric App ID
  GH_AS_BOT_INSTALLATION_ID   Numeric installation ID
  GH_AS_BOT_PRIVATE_KEY       PEM contents OR path to .pem file

Optional env:
  GITHUB_API_URL              Override API base (defaults to api.github.com)

Example:
  gh as-bot pr review 123 -c -b "Bot review here."
`

// Run dispatches the gh-as-bot subcommand. Splitting it from main lets
// tests drive the binary without forking, and matches gh-attach's layout.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	case "setup":
		return runSetup(stdin, stdout, stderr)
	case "--token":
		return runToken(stdout, stderr)
	case "doctor":
		return runDoctor(stdout, stderr)
	default:
		return runExec(args, stderr)
	}
}

func mintToken() (string, error) {
	cfg, err := app.LoadConfig()
	if err != nil {
		return "", err
	}
	jwt, err := app.MintAppJWT(cfg.AppID, cfg.PrivateKey, time.Now())
	if err != nil {
		return "", err
	}
	apiURL := os.Getenv("GITHUB_API_URL")
	tok, err := app.MintInstallationToken(context.Background(), nil, apiURL, jwt, cfg.InstallationID)
	if err != nil {
		return "", err
	}
	return tok.Token, nil
}

func runToken(stdout, stderr io.Writer) int {
	tok, err := mintToken()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, tok)
	return 0
}

func runDoctor(stdout, stderr io.Writer) int {
	tok, err := mintToken()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: config check failed:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "ok: minted installation token (length=%d)\n", len(tok))
	return 0
}

// runExec replaces the current process with `gh <args...>`, with
// GH_TOKEN swapped to the bot's installation token. Using exec (not a
// child process) means the user's terminal sees gh's exit code and
// signal handling directly — no wrapper layer in the way.
//
// Any inherited GITHUB_TOKEN is stripped so it can't shadow the bot
// credential we just minted.
func runExec(args []string, stderr io.Writer) int {
	tok, err := mintToken()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	bin, err := exec.LookPath("gh")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: gh not found in PATH")
		return 1
	}

	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "GH_TOKEN=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GH_TOKEN="+tok)

	if err := syscall.Exec(bin, append([]string{"gh"}, args...), env); err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: exec failed:", err)
		return 1
	}
	return 0
}
