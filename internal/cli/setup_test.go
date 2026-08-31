package cli

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enthus-appdev/gh-as-bot/internal/app"
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
	// Blank context + App ID + installation ID + nonexistent key path. The
	// key read should fail with a clear file-not-found message before we
	// hit any GitHub call.
	input := "\n12345\n67890\n/definitely/not/a/real/path.pem\n"
	var stdout, stderr bytes.Buffer
	code := runSetup(strings.NewReader(input), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (bad key path)", code)
	}
	if !strings.Contains(stderr.String(), "couldn't read key file") {
		t.Errorf("expected key-read error; got stderr: %q", stderr.String())
	}
}

// TestKeychainRoundTrip guards the macOS keychain bug: a raw multi-line
// PEM stored under `security ... -w` is hex-mangled on read because the
// value contains newlines. saveToKeychain base64-encodes to dodge this,
// and keychainKeyRef must decode on read. We verify the encoding is
// newline-free (the actual fix) and round-trips, and that the read-back
// snippet decodes it.
func TestKeychainRoundTrip(t *testing.T) {
	// Build a realistic multi-line PEM far larger than any base64 line-wrap
	// threshold (a real GitHub App key is ~1.7KB). This proves the encoder
	// emits a single newline-free line even at full key size — the property
	// security -w depends on.
	var b strings.Builder
	b.WriteString("-----BEGIN RSA PRIVATE KEY-----\n")
	for i := 0; i < 40; i++ {
		b.WriteString("MIIEpAIBAAKCAQEAytkEkF9UnK/X2mL7+8ou4F+WM+D+1c5ixPUW725irT+Uild\n")
	}
	b.WriteString("-----END RSA PRIVATE KEY-----\n")
	pem := []byte(b.String())

	// Exercise the PRODUCTION encoder, not a re-implementation — so this test
	// fails if saveToKeychain ever stops base64-encoding.
	encoded := encodeKeyForKeychain(pem)
	if strings.ContainsAny(encoded, "\n\r") {
		t.Fatalf("encoded key (%d bytes -> %d chars) contains a newline — security -w would hex-mangle it", len(pem), len(encoded))
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if string(decoded) != string(pem) {
		t.Errorf("round-trip mismatch through encodeKeyForKeychain")
	}

	ref := keychainServiceRef("gh-as-bot")
	if ref != "keychain:gh-as-bot" {
		t.Errorf("keychainServiceRef must be a lazy service reference; got %q", ref)
	}
	if strings.Contains(ref, "$(") {
		t.Errorf("keychainServiceRef must not query Keychain during shell startup; got %q", ref)
	}
}

func TestSetup_WritesNamedContext(t *testing.T) {
	t.Setenv("GH_AS_BOT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	pem := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(pem, []byte("dummy-pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	// context name, app id, installation id, key path, keychain "n".
	// The context is persisted before the network verify, so even though
	// verify fails offline, the entry must be on disk.
	input := "org\n12345\n67890\n" + pem + "\nn\n"
	var out, errb bytes.Buffer
	_ = runSetup(strings.NewReader(input), &out, &errb)

	cf, err := app.LoadContextFile()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, ok := cf.Contexts["org"]
	if !ok || e.AppID != "12345" || e.InstallationID != "67890" {
		t.Errorf("context not written correctly: %+v (ok=%v)", e, ok)
	}
	if !strings.Contains(out.String(), `wrote context "org"`) {
		t.Errorf("expected confirmation of context write; got: %q", out.String())
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
