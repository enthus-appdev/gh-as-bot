package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell_Bash(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	p := detectShell()
	if p.rcDescription != "~/.bashrc" {
		t.Errorf("rcDescription = %q, want ~/.bashrc", p.rcDescription)
	}
	lines := p.exportLines("1", "2", "/path/to/key.pem")
	if !strings.HasPrefix(lines[0], "export ") {
		t.Errorf("bash should emit `export`; got %q", lines[0])
	}
}

func TestDetectShell_Zsh(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/zsh")
	p := detectShell()
	if p.rcDescription != "~/.zshrc" {
		t.Errorf("rcDescription = %q, want ~/.zshrc", p.rcDescription)
	}
}

func TestDetectShell_Fish(t *testing.T) {
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	p := detectShell()
	if p.rcDescription != "~/.config/fish/config.fish" {
		t.Errorf("rcDescription = %q", p.rcDescription)
	}
	lines := p.exportLines("1", "2", "$(security find-generic-password -s gh-as-bot -w)")
	keyLine := lines[2]
	if !strings.HasPrefix(keyLine, "set -x ") {
		t.Errorf("fish should emit `set -x`; got %q", keyLine)
	}
	if strings.Contains(keyLine, "$(") {
		t.Errorf("fish command substitution should be (...) not $(...); got %q", keyLine)
	}
	if !strings.Contains(keyLine, "(security find-generic-password -s gh-as-bot -w)") {
		t.Errorf("fish should keep the substitution body; got %q", keyLine)
	}
}

func TestDetectShell_UnknownFallsBackToBashStyle(t *testing.T) {
	t.Setenv("SHELL", "/usr/local/bin/elvish")
	p := detectShell()
	if !strings.Contains(p.rcDescription, "~/.zshrc") || !strings.Contains(p.rcDescription, "~/.bashrc") {
		t.Errorf("unknown shell should mention both rc files; got %q", p.rcDescription)
	}
	lines := p.exportLines("1", "2", "/path/key.pem")
	if !strings.HasPrefix(lines[0], "export ") {
		t.Errorf("fallback should use bash-style export; got %q", lines[0])
	}
}

func TestExportLines_QuotesCommandSubstitution(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	p := detectShell()
	lines := p.exportLines("1", "2", "$(security find-generic-password -s gh-as-bot -w)")
	keyLine := lines[2]
	if !strings.Contains(keyLine, `"$(security`) {
		t.Errorf("bash command sub should be wrapped in double quotes; got %q", keyLine)
	}
}

func TestExportLines_LiteralPathUnquoted(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	p := detectShell()
	lines := p.exportLines("1", "2", "/abs/path/key.pem")
	keyLine := lines[2]
	if strings.Contains(keyLine, `"`) {
		t.Errorf("literal path should not be quoted; got %q", keyLine)
	}
}

func TestWriteManagedRcBlock_NewFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), "sub", ".bashrc")
	if err := writeManagedRcBlock(rc, "org", []string{"export GH_AS_BOT_PRIVATE_KEY_ORG=x"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readFile(t, rc)
	for _, want := range []string{"# >>> gh-as-bot:org >>>", "export GH_AS_BOT_PRIVATE_KEY_ORG=x", "# <<< gh-as-bot:org <<<"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestWriteManagedRcBlock_ReplaceIdempotent(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(rc, []byte("# pre-existing\nexport PATH=/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = writeManagedRcBlock(rc, "org", []string{"export K=v1"})
	_ = writeManagedRcBlock(rc, "org", []string{"export K=v2"})
	got := readFile(t, rc)
	if strings.Count(got, "# >>> gh-as-bot:org >>>") != 1 {
		t.Errorf("block duplicated:\n%s", got)
	}
	if strings.Contains(got, "export K=v1") || !strings.Contains(got, "export K=v2") {
		t.Errorf("block not replaced with latest:\n%s", got)
	}
	if !strings.Contains(got, "# pre-existing") || !strings.Contains(got, "export PATH=/x") {
		t.Errorf("surrounding content lost:\n%s", got)
	}
}

func TestWriteManagedRcBlock_MultipleContextsCoexist(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	_ = writeManagedRcBlock(rc, "org", []string{"export ORG=1"})
	_ = writeManagedRcBlock(rc, "personal", []string{"export PERSONAL=1"})
	// Rewriting org must not disturb personal.
	_ = writeManagedRcBlock(rc, "org", []string{"export ORG=2"})
	got := readFile(t, rc)
	for _, want := range []string{"# >>> gh-as-bot:org >>>", "export ORG=2", "# >>> gh-as-bot:personal >>>", "export PERSONAL=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "export ORG=1") {
		t.Errorf("stale org line survived:\n%s", got)
	}
}

func TestResolveRcTarget(t *testing.T) {
	cases := []struct{ ans, detected, want string }{
		{"", "/rc", "/rc"},
		{"y", "/rc", "/rc"},
		{"Y", "/rc", "/rc"},
		{"yes", "/rc", "/rc"},
		{"n", "/rc", ""},
		{"no", "/rc", ""},
		{"/other/path", "/rc", "/other/path"},
		{"", "", ""}, // unknown shell, accept default -> skip
	}
	for _, c := range cases {
		if got := resolveRcTarget(c.ans, c.detected); got != c.want {
			t.Errorf("resolveRcTarget(%q,%q) = %q, want %q", c.ans, c.detected, got, c.want)
		}
	}
}

func TestNeedsDeferredQuoting(t *testing.T) {
	for in, want := range map[string]bool{
		"$(security ...)":     true,
		"`whoami`":            true, // legacy backtick command sub
		"/abs/path/key.pem":   false,
		"-----BEGIN KEY-----": false,
	} {
		if got := needsDeferredQuoting(in); got != want {
			t.Errorf("needsDeferredQuoting(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWriteManagedRcBlock_RejectsBadID(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := writeManagedRcBlock(rc, "bad\nid", []string{"export X=1"}); err == nil {
		t.Error("expected error for id containing a newline")
	}
}

func TestWriteManagedRcBlock_MalformedExistingBlock(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	// begin marker present, end marker missing -> must error, not corrupt.
	if err := os.WriteFile(rc, []byte("# >>> gh-as-bot:org >>>\nexport X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedRcBlock(rc, "org", []string{"export X=2"}); err == nil {
		t.Error("expected malformed-block error")
	}
}

func TestWriteManagedRcBlock_StableAcrossRepeats(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(rc, []byte("export PATH=/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = writeManagedRcBlock(rc, "org", []string{"export K=v"})
	first := readFile(t, rc)
	_ = writeManagedRcBlock(rc, "org", []string{"export K=v"})
	_ = writeManagedRcBlock(rc, "org", []string{"export K=v"})
	if got := readFile(t, rc); got != first {
		t.Errorf("output not stable across repeats:\nfirst:\n%s\nlater:\n%s", first, got)
	}
	// mode preserved (0600 not widened to 0644)
	fi, err := os.Stat(rc)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600 (preserved)", fi.Mode().Perm())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
