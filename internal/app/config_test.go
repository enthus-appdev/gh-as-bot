package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_FromInlinePEM(t *testing.T) {
	t.Setenv("GH_AS_BOT_APP_ID", "1")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "2")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----\nABC\n-----END PRIVATE KEY-----")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.AppID != "1" || cfg.InstallationID != "2" {
		t.Errorf("ids not loaded: %+v", cfg)
	}
	if !strings.HasPrefix(string(cfg.PrivateKey), "-----BEGIN") {
		t.Errorf("inline PEM not preserved: %q", cfg.PrivateKey)
	}
}

func TestLoadConfig_FromPath(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "key.pem")
	contents := []byte("-----BEGIN PRIVATE KEY-----\nXYZ\n-----END PRIVATE KEY-----")
	if err := os.WriteFile(tmp, contents, 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	t.Setenv("GH_AS_BOT_APP_ID", "1")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "2")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", tmp)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if string(cfg.PrivateKey) != string(contents) {
		t.Errorf("file contents not loaded")
	}
}

func TestLoadConfig_MissingReportsAll(t *testing.T) {
	t.Setenv("GH_AS_BOT_APP_ID", "")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", "")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when all env missing")
	}
	for _, want := range []string{"GH_AS_BOT_APP_ID", "GH_AS_BOT_INSTALLATION_ID", "GH_AS_BOT_PRIVATE_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list %s; got: %v", want, err)
		}
	}
}
