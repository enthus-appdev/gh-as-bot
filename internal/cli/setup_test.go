package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetup_AbortsOnEmptyAppID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSetup(strings.NewReader("\n"), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "value required") {
		t.Errorf("expected required-value error; got stderr: %q", stderr.String())
	}
}

func TestSetup_HeaderAndStepsPrinted(t *testing.T) {
	var stdout, stderr bytes.Buffer
	_ = runSetup(strings.NewReader("\n"), &stdout, &stderr)
	out := stdout.String()
	for _, want := range []string{"gh-as-bot setup", "Step 1/4", "Pull requests", "App ID", "Homepage URL", "OAuth"} {
		if !strings.Contains(out, want) {
			t.Errorf("setup output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestSetup_ReadsKeyPathBeforeVerifying(t *testing.T) {
	// App ID + installation ID + nonexistent key path. The key read
	// should fail with a clear file-not-found message before we hit
	// any GitHub call.
	input := "12345\n67890\n/definitely/not/a/real/path.pem\n"
	var stdout, stderr bytes.Buffer
	code := runSetup(strings.NewReader(input), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (bad key path)", code)
	}
	if !strings.Contains(stderr.String(), "couldn't read key file") {
		t.Errorf("expected key-read error; got stderr: %q", stderr.String())
	}
}

func TestExpandTilde(t *testing.T) {
	got := expandTilde("/abs/path")
	if got != "/abs/path" {
		t.Errorf("absolute path mangled: %s", got)
	}
	got = expandTilde("~/foo")
	if !strings.HasSuffix(got, "/foo") || strings.HasPrefix(got, "~") {
		t.Errorf("tilde not expanded: %s", got)
	}
}
