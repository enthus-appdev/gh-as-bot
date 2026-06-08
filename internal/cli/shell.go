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

	// rcPath is the concrete file setup offers to write to (tilde-expanded).
	// Empty for an unrecognized shell — setup then asks for a path rather
	// than guessing. NOTE: $SHELL can lie (a login-shell zsh while the dev
	// works in bash), which is why setup confirms the path before writing
	// instead of trusting detection.
	rcPath string

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
		return shellProfile{rcDescription: "~/.zshrc", rcPath: expandTilde("~/.zshrc"), render: bashStyleExport}
	case "bash":
		return shellProfile{rcDescription: "~/.bashrc", rcPath: expandTilde("~/.bashrc"), render: bashStyleExport}
	case "fish":
		return shellProfile{rcDescription: "~/.config/fish/config.fish", rcPath: expandTilde("~/.config/fish/config.fish"), render: fishStyleExport}
	default:
		return shellProfile{rcDescription: "~/.zshrc, ~/.bashrc, or equivalent", rcPath: "", render: bashStyleExport}
	}
}

// rcBlockMarkers returns the begin/end marker lines for a managed gh-as-bot
// block, keyed by id (the context name, or "default" for legacy mode). Keying
// by id lets several contexts coexist in one rc — each setup run rewrites only
// its own block, never clobbering another context's key line.
func rcBlockMarkers(id string) (begin, end string) {
	return "# >>> gh-as-bot:" + id + " >>>", "# <<< gh-as-bot:" + id + " <<<"
}

// writeManagedRcBlock idempotently writes a marked block of lines into rcPath.
// An existing block with the same id is replaced in place; otherwise the block
// is appended. The rest of the file is left untouched. The file (and parent
// dir) are created if absent.
func writeManagedRcBlock(rcPath, id string, lines []string) error {
	begin, end := rcBlockMarkers(id)
	block := begin + "\n" + strings.Join(lines, "\n") + "\n" + end

	existing := ""
	if b, err := os.ReadFile(rcPath); err == nil {
		existing = string(b)
	} else if !os.IsNotExist(err) {
		return err
	}

	var out string
	if bi := strings.Index(existing, begin); bi != -1 {
		ei := strings.Index(existing[bi:], end)
		if ei == -1 {
			return fmt.Errorf("malformed gh-as-bot block in %s: found %q without %q", rcPath, begin, end)
		}
		ei += bi + len(end)
		// Drop a single trailing newline after the old block so replacement
		// doesn't accumulate blank lines across runs.
		if ei < len(existing) && existing[ei] == '\n' {
			ei++
		}
		out = existing[:bi] + block + "\n" + existing[ei:]
	} else {
		sep := ""
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			sep = "\n"
		}
		lead := "\n"
		if existing == "" {
			lead = ""
		}
		out = existing + sep + lead + block + "\n"
	}

	if dir := filepath.Dir(rcPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(rcPath, []byte(out), 0o644)
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
