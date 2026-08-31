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
  gh as-bot setup                       Walk through GitHub App configuration (start here)
  gh as-bot [--context <name>] <gh args...>   Run gh with bot credentials
  gh as-bot [--context <name>] --token        Print a fresh installation token to stdout
  gh as-bot [--context <name>] doctor         Verify config and credentials
  gh as-bot context <list|current|add|remove|export>   Manage named bot identities
  gh as-bot help                        Show this message

Selecting an identity:
  With contexts defined, pass --context <name> or set GH_AS_BOT_CONTEXT.
  With no contexts, gh-as-bot falls back to the legacy single-App env below.

Context env (per context <name>, uppercased):
  GH_AS_BOT_CONTEXT           Active context when --context is not passed
  GH_AS_BOT_PRIVATE_KEY_<NAME> PEM, path, or keychain:<service> reference

Legacy env (no contexts defined):
  GH_AS_BOT_APP_ID            Numeric App ID
  GH_AS_BOT_INSTALLATION_ID   Numeric installation ID
  GH_AS_BOT_PRIVATE_KEY       PEM, path, or keychain:<service> reference

Optional env:
  GITHUB_API_URL              Override API base (defaults to api.github.com)
  GH_AS_BOT_CONFIG            Config file path (default ~/.config/gh-as-bot/config.json)

Example:
  gh as-bot --context org pr review 123 -c -b "Bot review here."
`

// Run dispatches the gh-as-bot subcommand. Splitting it from main lets
// tests drive the binary without forking, and matches gh-attach's layout.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// extractContextFlag tolerates an empty slice, and a dangling "--context"
	// with no value leaves args empty — both fall through to the usage guard.
	ctxName, args := extractContextFlag(args)
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
	case "context":
		return runContext(ctxName, args[1:], stdout, stderr)
	case "--token":
		return runToken(ctxName, stdout, stderr)
	case "doctor":
		return runDoctor(ctxName, stdout, stderr)
	default:
		return runExec(ctxName, args, stderr)
	}
}

// extractContextFlag pulls a leading "--context NAME" / "--context=NAME"
// off the front of args, returning the context and the remaining args.
// Only the leading position is honored so it can never shadow a flag that
// belongs to gh itself.
func extractContextFlag(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	if args[0] == "--context" {
		if len(args) >= 2 {
			return args[1], args[2:]
		}
		return "", args[1:] // dangling "--context" with no value -> empty args -> usage
	}
	if strings.HasPrefix(args[0], "--context=") {
		return strings.TrimPrefix(args[0], "--context="), args[1:]
	}
	return "", args
}

func mintToken(ctxName string) (string, error) {
	cfg, err := app.ResolveConfig(ctxName)
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

func runToken(ctxName string, stdout, stderr io.Writer) int {
	tok, err := mintToken(ctxName)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, tok)
	return 0
}

func runDoctor(ctxName string, stdout, stderr io.Writer) int {
	tok, err := mintToken(ctxName)
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
func runExec(ctxName string, args []string, stderr io.Writer) int {
	tok, err := mintToken(ctxName)
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
