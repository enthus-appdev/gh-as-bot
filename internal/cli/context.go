package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"text/tabwriter"

	"github.com/enthus-appdev/gh-as-bot/internal/app"
)

const contextUsage = `gh as-bot context — manage named bot identities.

  context list                 List defined contexts
  context current              Show the active context (--context / GH_AS_BOT_CONTEXT)
  context add <name> --app-id <id> --installation-id <id>
  context remove <name>
  context export <name>        Print shell lines to activate a context
`

// keychainService is the macOS keychain service name for a context's key.
// Legacy (unnamed) setup uses the bare "gh-as-bot".
func keychainService(contextName string) string {
	if contextName == "" {
		return "gh-as-bot"
	}
	return "gh-as-bot-" + contextName
}

// keychainServiceRef is a static shell-safe reference. gh-as-bot resolves it
// only when it needs a token, so starting an unrelated shell never touches the
// user's keychain.
func keychainServiceRef(service string) string {
	return app.KeychainRef(service)
}

func runContext(ctxName string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, contextUsage)
		return 2
	}
	switch args[0] {
	case "list":
		return contextList(stdout, stderr)
	case "current":
		return contextCurrent(ctxName, stdout)
	case "add":
		return contextAdd(args[1:], stdout, stderr)
	case "remove":
		return contextRemove(args[1:], stdout, stderr)
	case "export":
		return contextExport(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprint(stderr, contextUsage)
		return 2
	}
}

func contextList(stdout, stderr io.Writer) int {
	cf, err := app.LoadContextFile()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	if len(cf.Contexts) == 0 {
		_, _ = fmt.Fprintln(stdout, "no contexts defined (legacy env mode)")
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tAPP_ID\tINSTALLATION_ID")
	for _, n := range cf.SortedNames() {
		e := cf.Contexts[n]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", n, e.AppID, e.InstallationID)
	}
	_ = tw.Flush()
	return 0
}

func contextCurrent(ctxName string, stdout io.Writer) int {
	if ctxName != "" {
		_, _ = fmt.Fprintln(stdout, ctxName)
		return 0
	}
	if v := os.Getenv("GH_AS_BOT_CONTEXT"); v != "" {
		_, _ = fmt.Fprintln(stdout, v)
		return 0
	}
	_, _ = fmt.Fprintln(stdout, "no context selected (use --context or set GH_AS_BOT_CONTEXT)")
	return 0
}

func contextAdd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: usage: context add <name> --app-id <id> --installation-id <id>")
		return 2
	}
	name := args[0]
	if err := app.ValidateContextName(name); err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 2
	}
	appID, instID, err := parseAddFlags(args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 2
	}
	cf, err := app.LoadContextFile()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	if other := cf.CollidingContext(name); other != "" {
		_, _ = fmt.Fprintf(stderr, "gh-as-bot: context %q collides with existing %q — both map to %s, so they'd share one key. Pick a name that differs by more than case or -/_.\n", name, other, app.KeyEnvVar(name))
		return 2
	}
	cf.Contexts[name] = app.ContextEntry{AppID: appID, InstallationID: instID}
	if err := app.SaveContextFile(cf); err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "added context %q (app_id=%s, installation_id=%s)\n", name, appID, instID)
	_, _ = fmt.Fprintf(stdout, "next: stash the key in keychain service %q, then `gh as-bot context export %s`\n", keychainService(name), name)
	return 0
}

func contextRemove(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: usage: context remove <name>")
		return 2
	}
	name := args[0]
	cf, err := app.LoadContextFile()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	if _, ok := cf.Contexts[name]; !ok {
		_, _ = fmt.Fprintf(stderr, "gh-as-bot: no such context %q\n", name)
		return 1
	}
	delete(cf.Contexts, name)
	if err := app.SaveContextFile(cf); err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "removed context %q\n", name)
	return 0
}

func contextExport(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot: usage: context export <name>")
		return 2
	}
	name := args[0]
	cf, err := app.LoadContextFile()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh-as-bot:", err)
		return 1
	}
	if _, ok := cf.Contexts[name]; !ok {
		_, _ = fmt.Fprintf(stderr, "gh-as-bot: no such context %q\n", name)
		return 1
	}
	// Keychain references are macOS-only (matches setup.go). On other platforms
	// emit a path placeholder rather than an unusable keychain reference.
	if runtime.GOOS == "darwin" {
		_, _ = fmt.Fprintf(stdout, "export %s=%q\n", app.KeyEnvVar(name), keychainServiceRef(keychainService(name)))
	} else {
		_, _ = fmt.Fprintf(stdout, "export %s=\"/path/to/%s-private-key.pem\"  # set to your key path or secrets-manager command\n", app.KeyEnvVar(name), name)
	}
	_, _ = fmt.Fprintf(stdout, "export GH_AS_BOT_CONTEXT=%q\n", name)
	return 0
}

// parseAddFlags reads --app-id / --installation-id (order-independent).
func parseAddFlags(args []string) (appID, instID string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--app-id":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--app-id requires a value")
			}
			appID = args[i+1]
			i++
		case "--installation-id":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--installation-id requires a value")
			}
			instID = args[i+1]
			i++
		default:
			return "", "", fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if appID == "" || instID == "" {
		return "", "", fmt.Errorf("both --app-id and --installation-id are required")
	}
	return appID, instID, nil
}
