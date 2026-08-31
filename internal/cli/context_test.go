package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContext_AddListRemove(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer

	if code := runContext("", []string{"add", "org", "--app-id", "100", "--installation-id", "200"}, &out, &errb); code != 0 {
		t.Fatalf("add failed: code=%d err=%s", code, errb.String())
	}

	out.Reset()
	if code := runContext("", []string{"list"}, &out, &errb); code != 0 {
		t.Fatalf("list failed: %s", errb.String())
	}
	if !strings.Contains(out.String(), "org") || !strings.Contains(out.String(), "200") {
		t.Errorf("list missing context: %q", out.String())
	}

	if code := runContext("", []string{"remove", "org"}, &out, &errb); code != 0 {
		t.Fatalf("remove failed: %s", errb.String())
	}
	out.Reset()
	_ = runContext("", []string{"list"}, &out, &errb)
	if strings.Contains(out.String(), "org") {
		t.Errorf("context not removed: %q", out.String())
	}
}

func TestContext_AddRejectsBadName(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	if code := runContext("", []string{"add", "1bad", "--app-id", "1", "--installation-id", "2"}, &out, &errb); code == 0 {
		t.Error("expected non-zero exit for bad name")
	}
}

func TestContext_AddRequiresBothFlags(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	if code := runContext("", []string{"add", "org", "--app-id", "1"}, &out, &errb); code == 0 {
		t.Error("expected non-zero exit when --installation-id missing")
	}
}

func TestContext_AddRejectsMissingFlagValues(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Run("missing app-id value", func(t *testing.T) {
		var out, errb bytes.Buffer
		code := runContext("", []string{"add", "org", "--installation-id", "2", "--app-id"}, &out, &errb)
		if code == 0 {
			t.Error("expected failure when --app-id is missing a value")
		}
		if !strings.Contains(errb.String(), "requires a value") {
			t.Errorf("expected validation error, got: %s", errb.String())
		}
	})
	t.Run("missing installation-id value", func(t *testing.T) {
		var out, errb bytes.Buffer
		code := runContext("", []string{"add", "org", "--app-id", "1", "--installation-id"}, &out, &errb)
		if code == 0 {
			t.Error("expected failure when --installation-id is missing a value")
		}
	})
}

func TestContext_AddRejectsUnknownFlag(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	code := runContext("", []string{"add", "org", "--app-id", "1", "--installation-id", "2", "--unknown-flag"}, &out, &errb)
	if code == 0 {
		t.Error("expected non-zero exit when unknown flag is provided")
	}
	if !strings.Contains(errb.String(), "unknown flag") {
		t.Errorf("expected unknown flag error, got %q", errb.String())
	}
}

func TestContext_AddRejectsKeyEnvVarCollision(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	if code := runContext("", []string{"add", "org", "--app-id", "1", "--installation-id", "2"}, &out, &errb); code != 0 {
		t.Fatalf("first add failed: %s", errb.String())
	}
	// "Org" maps to the same GH_AS_BOT_PRIVATE_KEY_ORG as "org" -> must reject.
	errb.Reset()
	if code := runContext("", []string{"add", "Org", "--app-id", "3", "--installation-id", "4"}, &out, &errb); code == 0 {
		t.Error("expected collision rejection for case-variant name")
	}
	if !strings.Contains(errb.String(), "collides") {
		t.Errorf("expected collision error, got %q", errb.String())
	}
	// re-adding the SAME name (upsert) must still be allowed.
	if code := runContext("", []string{"add", "org", "--app-id", "9", "--installation-id", "8"}, &out, &errb); code != 0 {
		t.Errorf("re-adding same name should upsert, got exit %d", code)
	}
}

func TestContext_NoArgs(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runContext("", []string{}, &out, &errb); code != 2 {
		t.Errorf("expected exit 2 for no subcommand, got %d", code)
	}
	if !strings.Contains(errb.String(), "manage named bot identities") {
		t.Errorf("expected context usage, got %q", errb.String())
	}
}

func TestContext_RemoveUnknown(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	if code := runContext("", []string{"remove", "ghost"}, &out, &errb); code == 0 {
		t.Error("expected non-zero exit removing unknown context")
	}
}

func TestContext_Export(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	_ = runContext("", []string{"add", "org", "--app-id", "100", "--installation-id", "200"}, &out, &errb)
	out.Reset()
	if code := runContext("", []string{"export", "org"}, &out, &errb); code != 0 {
		t.Fatalf("export failed: %s", errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "GH_AS_BOT_PRIVATE_KEY_ORG") {
		t.Errorf("export missing key env var: %q", s)
	}
	if !strings.Contains(s, `GH_AS_BOT_CONTEXT="org"`) {
		t.Errorf("export missing context selector: %q", s)
	}
	// Keychain sourcing is macOS-only; other platforms get a path placeholder.
	if runtime.GOOS == "darwin" {
		if !strings.Contains(s, `"keychain:gh-as-bot-org"`) {
			t.Errorf("darwin export missing keychain snippet: %q", s)
		}
		if strings.Contains(s, "$(") {
			t.Errorf("darwin export must not query Keychain during shell startup: %q", s)
		}
	} else {
		if !strings.Contains(s, "/path/to/org-private-key.pem") {
			t.Errorf("non-darwin export missing path placeholder: %q", s)
		}
	}
}

func TestContext_ExportErrors(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Run("no arguments", func(t *testing.T) {
		var out, errb bytes.Buffer
		code := runContext("", []string{"export"}, &out, &errb)
		if code != 2 {
			t.Errorf("expected exit code 2, got %d", code)
		}
	})
	t.Run("unknown context", func(t *testing.T) {
		var out, errb bytes.Buffer
		code := runContext("", []string{"export", "unknown-ctx"}, &out, &errb)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
		if !strings.Contains(errb.String(), "no such context") {
			t.Errorf("expected 'no such context' error, got %q", errb.String())
		}
	})
}

func TestContext_ListEmpty(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var out, errb bytes.Buffer
	if code := runContext("", []string{"list"}, &out, &errb); code != 0 {
		t.Fatalf("list failed: %s", errb.String())
	}
	if !strings.Contains(out.String(), "no contexts defined") {
		t.Errorf("empty list message wrong: %q", out.String())
	}
}

func TestContext_Current(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("GH_AS_BOT_CONTEXT", "env-ctx")
		var out bytes.Buffer
		if code := runContext("flag-ctx", []string{"current"}, &out, io.Discard); code != 0 {
			t.Fatalf("current failed: code=%d", code)
		}
		if !strings.Contains(out.String(), "flag-ctx") {
			t.Errorf("expected flag-ctx, got %q", out.String())
		}
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("GH_AS_BOT_CONTEXT", "my-context")
		var out bytes.Buffer
		_ = runContext("", []string{"current"}, &out, io.Discard)
		if !strings.Contains(out.String(), "my-context") {
			t.Errorf("expected my-context, got %q", out.String())
		}
	})
	t.Run("none selected", func(t *testing.T) {
		t.Setenv("GH_AS_BOT_CONTEXT", "")
		var out bytes.Buffer
		_ = runContext("", []string{"current"}, &out, io.Discard)
		if !strings.Contains(out.String(), "no context selected") {
			t.Errorf("expected warning, got %q", out.String())
		}
	})
}
