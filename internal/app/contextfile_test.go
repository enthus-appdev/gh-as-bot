package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAndKeyEnvVar(t *testing.T) {
	cases := []struct{ name, env string }{
		{"org", "GH_AS_BOT_PRIVATE_KEY_ORG"},
		{"personal", "GH_AS_BOT_PRIVATE_KEY_PERSONAL"},
		{"my-org", "GH_AS_BOT_PRIVATE_KEY_MY_ORG"},
		{"a1b2", "GH_AS_BOT_PRIVATE_KEY_A1B2"},
	}
	for _, c := range cases {
		if got := KeyEnvVar(c.name); got != c.env {
			t.Errorf("KeyEnvVar(%q) = %q, want %q", c.name, got, c.env)
		}
	}
}

func TestValidateContextName(t *testing.T) {
	good := []string{"org", "my-org", "ctx_1", "A"}
	for _, n := range good {
		if err := ValidateContextName(n); err != nil {
			t.Errorf("ValidateContextName(%q) unexpected error: %v", n, err)
		}
	}
	bad := []string{"", "1abc", "has space", "wei?rd", "-leading"}
	for _, n := range bad {
		if err := ValidateContextName(n); err == nil {
			t.Errorf("ValidateContextName(%q) = nil, want error", n)
		}
	}
}

func TestConfigPath_Override(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", "/tmp/x/config.json")
	if got := ConfigPath(); got != "/tmp/x/config.json" {
		t.Errorf("ConfigPath() = %q, want override", got)
	}
}

func TestConfigPath_DefaultHome(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	got := ConfigPath()
	suffix := filepath.Join(".config", "gh-as-bot", "config.json")
	if !strings.HasSuffix(got, suffix) {
		t.Errorf("ConfigPath() = %q, want suffix %q", got, suffix)
	}
}

func TestConfigPath_XDG(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got := ConfigPath(); got != "/tmp/xdg/gh-as-bot/config.json" {
		t.Errorf("ConfigPath() = %q, want XDG path", got)
	}
}

func TestLoadContextFile_MissingIsEmpty(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "nope.json"))
	cf, err := LoadContextFile()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(cf.Contexts) != 0 {
		t.Errorf("expected empty contexts, got %v", cf.Contexts)
	}
}

func TestLoadContextFile_EmptyIsNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GH_AS_BOT_CONFIG", path)
	for _, content := range []string{"", "   \n\t "} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		cf, err := LoadContextFile()
		if err != nil {
			t.Fatalf("empty/whitespace file should not error (content=%q): %v", content, err)
		}
		if len(cf.Contexts) != 0 {
			t.Errorf("expected empty contexts for content=%q, got %v", content, cf.Contexts)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "sub", "config.json"))
	cf := &ContextFile{Contexts: map[string]ContextEntry{
		"org": {AppID: "1", InstallationID: "2"},
	}}
	if err := SaveContextFile(cf); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadContextFile()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Contexts["org"].AppID != "1" || got.Contexts["org"].InstallationID != "2" {
		t.Errorf("round-trip mismatch: %+v", got.Contexts["org"])
	}
}
