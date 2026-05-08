package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_NoArgsShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, &stdout, &stderr)
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
		code := Run([]string{flag}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("%s exit code = %d, want 0", flag, code)
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("%s should print usage to stdout", flag)
		}
	}
}

func TestRun_TokenFailsWithoutConfig(t *testing.T) {
	t.Setenv("GH_AS_BOT_APP_ID", "")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", "")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--token"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (missing config)", code)
	}
	if !strings.Contains(stderr.String(), "missing required env") {
		t.Errorf("stderr should report missing env; got: %q", stderr.String())
	}
}
