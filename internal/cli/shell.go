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
	return []string{
		p.render("GH_AS_BOT_APP_ID", appID, false),
		p.render("GH_AS_BOT_INSTALLATION_ID", instID, false),
		p.render("GH_AS_BOT_PRIVATE_KEY", keyRef, needsDeferredQuoting(keyRef)),
	}
}

// keyExportLine renders a single per-context key export, the one source of
// truth for how a keyRef is quoted (so the context-mode and legacy paths can't
// diverge).
func (p shellProfile) keyExportLine(envVar, keyRef string) string {
	return p.render(envVar, keyRef, needsDeferredQuoting(keyRef))
}

// needsDeferredQuoting reports whether a keyRef must be quoted so the shell
// defers evaluation to read-time rather than profile-load time. Both `$(...)`
// and legacy backtick command substitution qualify; a literal path does not.
func needsDeferredQuoting(keyRef string) bool {
	return strings.HasPrefix(keyRef, "$(") || strings.Contains(keyRef, "`")
}

// resolveRcTarget maps a [Y/n]-or-path answer to the rc file to write:
// blank / y / yes keep the detected path; n / no skip (return ""); anything
// else is treated as a path override (tilde-expanded).
func resolveRcTarget(answer, detected string) string {
	switch {
	case strings.EqualFold(answer, "n"), strings.EqualFold(answer, "no"):
		return ""
	case answer == "", strings.EqualFold(answer, "y"), strings.EqualFold(answer, "yes"):
		return detected
	default:
		return expandTilde(answer)
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
// is appended, separated from surrounding content by a single blank line. The
// boundary normalization makes repeated writes stable (no blank-line growth),
// and the rest of the file is left untouched. The file (and parent dir) are
// created if absent.
//
// The write is atomic (temp file + rename in the same dir) so a crash or full
// disk can never leave the user's dotfile truncated, and the existing file's
// permission mode is preserved (we don't widen a 0600 rc to 0644).
func writeManagedRcBlock(rcPath, id string, lines []string) error {
	// id flows into the marker comment lines; a newline/NUL would break the
	// block structure (and could inject into a sourced file). Callers validate
	// context names, but guard here too since this writes shell config.
	if strings.ContainsAny(id, "\n\r\x00") {
		return fmt.Errorf("invalid block id %q", id)
	}
	begin, end := rcBlockMarkers(id)
	block := begin + "\n" + strings.Join(lines, "\n") + "\n" + end

	existing := ""
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(rcPath); err == nil {
		mode = fi.Mode().Perm()
		b, rerr := os.ReadFile(rcPath)
		if rerr != nil {
			return rerr
		}
		existing = string(b)
	} else if !os.IsNotExist(err) {
		return err
	}

	var before, after string
	if bi := strings.Index(existing, begin); bi != -1 {
		rest := strings.Index(existing[bi:], end)
		if rest == -1 {
			return fmt.Errorf("malformed gh-as-bot block in %s: found %q without %q", rcPath, begin, end)
		}
		before = existing[:bi]
		after = existing[bi+rest+len(end):]
	} else {
		before = existing
	}

	// Join non-empty segments with a single blank line; deterministic on repeat.
	parts := make([]string, 0, 3)
	if s := strings.TrimRight(before, "\n"); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts, block)
	if s := strings.TrimLeft(after, "\n"); s != "" {
		parts = append(parts, s)
	}
	out := strings.Join(parts, "\n\n") + "\n"

	dir := filepath.Dir(rcPath)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".gh-as-bot-rc-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.WriteString(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, rcPath)
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
		// Quote command substitutions so whitespace and newlines remain one value.
		// Shells still evaluate the command when the profile is sourced.
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
