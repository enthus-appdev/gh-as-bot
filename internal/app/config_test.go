package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPEM = "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----\n"

// isolateConfig points GH_AS_BOT_CONFIG at an absent file so a real
// ~/.config/gh-as-bot/config.json on the dev machine can't influence the
// test, and clears the context selector env.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("GH_AS_BOT_CONTEXT", "")
}

func TestLoadConfig_FromInlinePEM(t *testing.T) {
	isolateConfig(t)
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
	isolateConfig(t)
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

func TestKeychainRef(t *testing.T) {
	if got := KeychainRef("gh-as-bot-org"); got != "keychain:gh-as-bot-org" {
		t.Fatalf("KeychainRef = %q", got)
	}
}

func TestLoadKey_EmptyKeychainService(t *testing.T) {
	_, err := loadKey("keychain:")
	if err == nil || !strings.Contains(err.Error(), "service is empty") {
		t.Fatalf("expected empty-service error, got %v", err)
	}
}

func TestLoadConfig_MissingReportsAll(t *testing.T) {
	isolateConfig(t)
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

// --- context resolution ---

func writeCtxFile(t *testing.T) {
	t.Helper()
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cf := &ContextFile{Contexts: map[string]ContextEntry{
		"org":      {AppID: "100", InstallationID: "200"},
		"personal": {AppID: "300", InstallationID: "400"},
	}}
	if err := SaveContextFile(cf); err != nil {
		t.Fatalf("setup save: %v", err)
	}
}

func clearLegacyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GH_AS_BOT_APP_ID", "")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_ORG", "")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_PERSONAL", "")
	t.Setenv("GH_AS_BOT_CONTEXT", "")
}

func TestResolve_FlagContext(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_ORG", testPEM)
	cfg, err := ResolveConfig("org")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.AppID != "100" || cfg.InstallationID != "200" || len(cfg.PrivateKey) == 0 {
		t.Errorf("wrong cfg: %+v", cfg)
	}
}

func TestResolve_EnvContext(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	t.Setenv("GH_AS_BOT_CONTEXT", "personal")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_PERSONAL", testPEM)
	cfg, err := ResolveConfig("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.AppID != "300" {
		t.Errorf("expected personal app id, got %q", cfg.AppID)
	}
}

func TestResolve_FlagBeatsEnv(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	t.Setenv("GH_AS_BOT_CONTEXT", "personal")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_ORG", testPEM)
	cfg, err := ResolveConfig("org") // flag wins over env
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cfg.AppID != "100" {
		t.Errorf("flag should win; got app id %q", cfg.AppID)
	}
}

func TestResolve_UnknownContext(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	_, err := ResolveConfig("nope")
	if err == nil || !strings.Contains(err.Error(), "unknown context") {
		t.Errorf("want unknown-context error, got %v", err)
	}
}

func TestResolve_ContextSelectedButKeyMissing(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	// bare legacy key set, but it must NOT be used as a fallback
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", testPEM)
	_, err := ResolveConfig("org")
	if err == nil || !strings.Contains(err.Error(), "GH_AS_BOT_PRIVATE_KEY_ORG") {
		t.Errorf("want missing-key error naming the per-context var, got %v", err)
	}
}

func TestResolve_ContextsDefinedNoneSelected(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	_, err := ResolveConfig("")
	if err == nil || !strings.Contains(err.Error(), "no context selected") {
		t.Errorf("want no-context-selected error, got %v", err)
	}
}

func TestResolve_IncompleteContext(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	cf := &ContextFile{Contexts: map[string]ContextEntry{
		"bad-app":  {AppID: "", InstallationID: "200"},
		"bad-inst": {AppID: "100", InstallationID: ""},
	}}
	if err := SaveContextFile(cf); err != nil {
		t.Fatalf("setup save: %v", err)
	}
	clearLegacyEnv(t)
	_, err := ResolveConfig("bad-app")
	if err == nil || !strings.Contains(err.Error(), "missing app_id") {
		t.Errorf("expected missing app_id error, got %v", err)
	}
	_, err = ResolveConfig("bad-inst")
	if err == nil || !strings.Contains(err.Error(), "missing installation_id") {
		t.Errorf("expected missing installation_id error, got %v", err)
	}
}

func TestResolve_InvalidPrivateKey(t *testing.T) {
	writeCtxFile(t)
	clearLegacyEnv(t)
	// not inline PEM and not an existing path -> loadKey fails
	t.Setenv("GH_AS_BOT_PRIVATE_KEY_ORG", "/definitely/not/a/real/path.pem")
	_, err := ResolveConfig("org")
	if err == nil || !strings.Contains(err.Error(), "load private key") {
		t.Errorf("expected load private key error, got %v", err)
	}
}

func TestResolve_CorruptedConfigFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GH_AS_BOT_CONFIG", configPath)
	if err := os.WriteFile(configPath, []byte("{invalid-json}"), 0o600); err != nil {
		t.Fatalf("setup write: %v", err)
	}
	clearLegacyEnv(t)
	_, err := ResolveConfig("")
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse-config error, got %v", err)
	}
}

func TestResolve_LegacyMode(t *testing.T) {
	// no config file at all -> legacy env behavior
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	clearLegacyEnv(t)
	t.Setenv("GH_AS_BOT_APP_ID", "9")
	t.Setenv("GH_AS_BOT_INSTALLATION_ID", "8")
	t.Setenv("GH_AS_BOT_PRIVATE_KEY", testPEM)
	cfg, err := ResolveConfig("")
	if err != nil {
		t.Fatalf("legacy resolve: %v", err)
	}
	if cfg.AppID != "9" || cfg.InstallationID != "8" {
		t.Errorf("legacy cfg wrong: %+v", cfg)
	}
}
