package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractContextFlag(t *testing.T) {
	cases := []struct {
		in       []string
		wantCtx  string
		wantArgs []string
	}{
		{[]string{"--context", "org", "pr", "review"}, "org", []string{"pr", "review"}},
		{[]string{"--context=org", "pr"}, "org", []string{"pr"}},
		{[]string{"pr", "--context", "org"}, "", []string{"pr", "--context", "org"}}, // only leading
		{[]string{"doctor"}, "", []string{"doctor"}},
		{[]string{"--context"}, "", []string{}}, // dangling flag, no value
	}
	for _, c := range cases {
		ctx, args := extractContextFlag(c.in)
		if ctx != c.wantCtx {
			t.Errorf("in %v: ctx = %q, want %q", c.in, ctx, c.wantCtx)
		}
		if strings.Join(args, " ") != strings.Join(c.wantArgs, " ") {
			t.Errorf("in %v: args = %v, want %v", c.in, args, c.wantArgs)
		}
	}
}

func TestRun_NoArgsShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("stderr should contain usage; got: %q", stderr.String())
	}
}

func TestRun_HelpFlag(t *testing.T) {
	for _, flag := range []string{"help", "-h", "--help"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{flag}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Errorf("%s exit code = %d, want 0", flag, code)
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("%s should print usage to stdout", flag)
		}
		if !strings.Contains(stdout.String(), "setup") {
			t.Errorf("%s usage should mention setup subcommand", flag)
		}
	}
}

func TestRun_DanglingContextShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--context"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit 2 for dangling --context, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage message, got %q", stderr.String())
	}
}

func TestRun_TokenFailsWithoutConfig(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("GH_AS_BOT_CONTEXT", "")
	t.Setenv("GH_AS_BOT_APP_ID", "")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--token"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (missing config)", code)
	}
	if !strings.Contains(stderr.String(), "missing required env") {
		t.Errorf("stderr should report missing env; got: %q", stderr.String())
	}
}

func TestRun_SetupRoutesToSetup(t *testing.T) {
	// Empty stdin → first prompt aborts, but we exercise dispatch.
	var stdout, stderr bytes.Buffer
	code := Run([]string{"setup"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (empty input aborts)", code)
	}
	if !strings.Contains(stdout.String(), "gh-as-bot setup") {
		t.Errorf("setup header should appear; got: %q", stdout.String())
	}
}
