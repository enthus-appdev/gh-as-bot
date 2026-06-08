package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/enthus-appdev/gh-as-bot/internal/app"
)

// runSetup walks the user through configuring gh-as-bot. It is a guided
// alternative to setting GH_AS_BOT_* by hand. The flow:
//  1. Point the user at GitHub's "New App" page (with required perms documented)
//  2. Collect App ID, installation ID, and a path to the .pem private key
//  3. Verify the credentials by minting a real installation token
//  4. Optionally store the key in the macOS keychain
//  5. Print a shell-profile snippet the user can paste into ~/.zshrc
//
// The verification step (3) is the most important — it catches typos,
// uninstalled Apps, and wrong permissions before the user wires anything
// into their shell.
func runSetup(stdin io.Reader, stdout, stderr io.Writer) int {
	s := &setup{
		in:  bufio.NewReader(stdin),
		out: stdout,
		err: stderr,
	}
	return s.run()
}

type setup struct {
	in  *bufio.Reader
	out io.Writer
	err io.Writer
}

const setupHeader = `gh-as-bot setup
═══════════════════════════════════════════════════════════════════════
This walks you through configuring a GitHub App so gh-as-bot can post
PR reviews and comments under a bot identity.

You'll need:
  - Admin access to the org where the App will live
  - About 5 minutes
`

func (s *setup) run() int {
	_, _ = fmt.Fprint(s.out, setupHeader)

	// Step 1
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "Step 1/4 — Create your GitHub App (one App per person, recommended)")
	_, _ = fmt.Fprintln(s.out, "  Per-person Apps give each developer their own bot identity")
	_, _ = fmt.Fprintln(s.out, "  (e.g. `<your-username>-claude[bot]`), so PR comments are attributed")
	_, _ = fmt.Fprintln(s.out, "  to whoever's session produced them. No shared private keys.")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Open this URL (creates the App under YOUR account):")
	_, _ = fmt.Fprintln(s.out, "    https://github.com/settings/apps/new")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Suggested name: <your-username>-claude  (must be globally unique)")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Required form fields (GitHub blocks save until these are set):")
	_, _ = fmt.Fprintln(s.out, "    - Homepage URL: any valid URL incl. scheme, e.g. https://example.com")
	_, _ = fmt.Fprintln(s.out, "      (cosmetic for our use — it just has to parse as a URL)")
	_, _ = fmt.Fprintln(s.out, "    - Uncheck \"Request user authorization (OAuth) during installation\".")
	_, _ = fmt.Fprintln(s.out, "      Leaving it checked forces a Callback URL we don't use — gh-as-bot")
	_, _ = fmt.Fprintln(s.out, "      authenticates as an App installation, not via the OAuth user flow.")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Required Repository permissions:")
	_, _ = fmt.Fprintln(s.out, "    - Pull requests:  Read & write   (post reviews / review comments)")
	_, _ = fmt.Fprintln(s.out, "    - Contents:       Read           (gh needs this for most read paths)")
	_, _ = fmt.Fprintln(s.out, "    - Issues:         Read & write   (post issue / PR conversation comments)")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Webhook: uncheck \"Active\" — we don't use webhooks.")
	_, _ = fmt.Fprintln(s.out, "  \"Where can this GitHub App be installed?\": Any account.")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  After saving the App:")
	_, _ = fmt.Fprintln(s.out, "    a) Generate a private key (.pem) and download it.")
	_, _ = fmt.Fprintln(s.out, "    b) Install the App on the org/repos that should accept bot reviews.")
	_, _ = fmt.Fprintln(s.out, "    c) Note the App ID (App settings header) and installation ID")
	_, _ = fmt.Fprintln(s.out, "       (visible in the URL after install: .../installations/<id>/...)")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  (Want a shared org-owned bot instead? Use")
	_, _ = fmt.Fprintln(s.out, "   https://github.com/organizations/<your-org>/settings/apps/new — but")
	_, _ = fmt.Fprintln(s.out, "   you lose per-person attribution and share one private key.)")
	_, _ = fmt.Fprintln(s.out, "")

	appID, err := s.requirePrompt("App ID (numeric)")
	if err != nil {
		return 1
	}
	instID, err := s.requirePrompt("Installation ID (numeric)")
	if err != nil {
		return 1
	}
	keyPath, err := s.requirePrompt("Path to private key .pem (e.g. ~/Downloads/your-app.pem)")
	if err != nil {
		return 1
	}
	keyPath = expandTilde(keyPath)

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		_, _ = fmt.Fprintln(s.err, "  ✗ couldn't read key file:", err)
		return 1
	}

	// Step 2
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "Step 2/4 — Verify credentials")
	jwt, err := app.MintAppJWT(appID, keyData, time.Now())
	if err != nil {
		_, _ = fmt.Fprintln(s.err, "  ✗ JWT minting failed:", err)
		_, _ = fmt.Fprintln(s.err, "    Check that the .pem is the App private key (not an SSH key).")
		return 1
	}
	tok, err := app.MintInstallationToken(context.Background(), nil, "", jwt, instID)
	if err != nil {
		_, _ = fmt.Fprintln(s.err, "  ✗ token exchange failed:", err)
		_, _ = fmt.Fprintln(s.err, "    Check the App ID, installation ID, and that the App is installed on at least one repo.")
		return 1
	}
	_, _ = fmt.Fprintf(s.out, "  ✓ minted installation token (length=%d, expires_at=%s)\n", len(tok.Token), tok.ExpiresAt.Format(time.RFC3339))

	// Step 3
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "Step 3/4 — Store the private key")
	keyRef := keyPath
	if runtime.GOOS == "darwin" {
		_, _ = fmt.Fprintln(s.out, "  Keychain storage avoids leaving the .pem on disk in plaintext.")
		choice := s.prompt("Save key to macOS keychain? [Y/n]")
		if choice == "" || strings.EqualFold(choice, "y") || strings.EqualFold(choice, "yes") {
			if err := saveToKeychain(keyData); err != nil {
				_, _ = fmt.Fprintln(s.err, "  ✗ keychain save failed:", err)
				_, _ = fmt.Fprintln(s.out, "  Falling back to file path:", keyPath)
			} else {
				_, _ = fmt.Fprintln(s.out, "  ✓ saved to keychain (service=gh-as-bot, account=default)")
				_, _ = fmt.Fprintln(s.out, "  You can now safely delete the .pem file from disk.")
				keyRef = keychainKeyRef
			}
		} else {
			_, _ = fmt.Fprintln(s.out, "  Skipped — env will reference the .pem path directly.")
		}
	} else {
		_, _ = fmt.Fprintln(s.out, "  Keychain storage is macOS-only; on Linux consider direnv or your")
		_, _ = fmt.Fprintln(s.out, "  secrets manager. Env will reference the .pem path directly.")
	}

	// Step 4
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "Step 4/4 — Shell configuration")
	shell := detectShell()
	_, _ = fmt.Fprintf(s.out, "  Add this to your shell profile (%s):\n", shell.rcDescription)
	_, _ = fmt.Fprintln(s.out, "")
	for _, line := range shell.exportLines(appID, instID, keyRef) {
		_, _ = fmt.Fprintf(s.out, "    %s\n", line)
	}
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "  Open a fresh shell (or `source` your profile), then verify:")
	_, _ = fmt.Fprintln(s.out, "    gh as-bot doctor")
	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintln(s.out, "Setup complete. ✨")
	return 0
}

func (s *setup) prompt(label string) string {
	_, _ = fmt.Fprintf(s.out, "  %s: ", label)
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

func (s *setup) requirePrompt(label string) (string, error) {
	v := s.prompt(label)
	if v == "" {
		_, _ = fmt.Fprintln(s.err, "  ✗ value required, aborting setup")
		return "", errors.New("empty input")
	}
	return v, nil
}

// keychainKeyRef is the shell snippet that reads the stored key back into
// GH_AS_BOT_PRIVATE_KEY. The key is stored base64-encoded (see
// saveToKeychain), so it must be decoded on read. Apple's `base64 -D`
// (capital, decode) is used because it's present on every macOS version,
// unlike the GNU-style `-d`/`--decode`. This snippet is only ever emitted
// on darwin.
const keychainKeyRef = `$(security find-generic-password -s gh-as-bot -w | base64 -D)`

// saveToKeychain stores the PEM under service=gh-as-bot account=default.
// `-U` upserts, so re-running setup overwrites stale keys cleanly.
//
// The PEM is base64-encoded before storage. A raw multi-line PEM is a
// trap: macOS `security ... -w` hex-encodes any value containing a
// newline on read, so a raw PEM round-trips to a hex blob that LoadConfig
// then mistakes for a file path. base64 collapses the key to a single
// newline-free line, which `-w` returns verbatim. keychainKeyRef decodes it.
func saveToKeychain(pem []byte) error {
	encoded := base64.StdEncoding.EncodeToString(pem)
	cmd := exec.Command("security", "add-generic-password",
		"-s", "gh-as-bot",
		"-a", "default",
		"-U",
		"-w", encoded,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s: %w", msg, err)
	}
	return nil
}

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}
