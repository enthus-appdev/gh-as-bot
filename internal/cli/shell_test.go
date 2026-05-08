package cli

import (
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
