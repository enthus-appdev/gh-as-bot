package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// shellProfile describes how to render the env-export snippet for a
// detected shell. Bash and zsh share the same `export` syntax; fish uses
// `set -x`. Unknown shells fall back to the bash/zsh snippet with a
// generic rc-file description, which is correct for ~99% of dev setups.
type shellProfile struct {
	// rcDescription is the human-readable file path shown in the prompt
	// (e.g. "~/.bashrc"). For unknown shells we widen this to a list.
	rcDescription string

	// quotedAssign is the function that renders `KEY=value` (or its
	// equivalent) for this shell, given a value that may contain a
	// command substitution.
	render func(key, value string, isCommandSub bool) string
}

func (p shellProfile) exportLines(appID, instID, keyRef string) []string {
	keyIsSub := strings.HasPrefix(keyRef, "$(")
	return []string{
		p.render("GH_AS_BOT_APP_ID", appID, false),
		p.render("GH_AS_BOT_INSTALLATION_ID", instID, false),
		p.render("GH_AS_BOT_PRIVATE_KEY", keyRef, keyIsSub),
	}
}

// detectShell inspects $SHELL and returns a profile describing how to
// emit the export snippet. We deliberately keep the matching loose
// (basename suffix only) — exotic shell paths (/opt/homebrew/bin/zsh,
// custom builds, etc.) all resolve correctly that way.
func detectShell() shellProfile {
	switch shellBasename() {
	case "zsh":
		return shellProfile{rcDescription: "~/.zshrc", render: bashStyleExport}
	case "bash":
		return shellProfile{rcDescription: "~/.bashrc", render: bashStyleExport}
	case "fish":
		return shellProfile{rcDescription: "~/.config/fish/config.fish", render: fishStyleExport}
	default:
		return shellProfile{rcDescription: "~/.zshrc, ~/.bashrc, or equivalent", render: bashStyleExport}
	}
}

func shellBasename() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return ""
	}
	return filepath.Base(sh)
}

func bashStyleExport(key, value string, isCommandSub bool) string {
	if isCommandSub {
		// Quote command substitutions so the shell defers evaluation
		// until the variable is read, not at profile load.
		return fmt.Sprintf(`export %s="%s"`, key, value)
	}
	return fmt.Sprintf("export %s=%s", key, value)
}

func fishStyleExport(key, value string, isCommandSub bool) string {
	// fish uses `set -x` for exported vars. Command substitution in
	// fish is `(cmd)` not `$(cmd)`, so we rewrite if we see one.
	if isCommandSub {
		v := strings.TrimPrefix(value, "$(")
		v = strings.TrimSuffix(v, ")")
		return fmt.Sprintf("set -x %s (%s)", key, v)
	}
	return fmt.Sprintf("set -x %s %s", key, value)
}
