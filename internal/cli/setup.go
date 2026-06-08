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

	// A named context lets you keep more than one identity (e.g. org vs
	// personal). Blank keeps the legacy single-App env behavior.
	ctxName := s.prompt("Context name (blank for legacy single-App mode)")
	if ctxName != "" {
		if err := app.ValidateContextName(ctxName); err != nil {
			_, _ = fmt.Fprintln(s.err, "  ✗", err)
			return 1
		}
	}

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

	// Persist the context definition (non-secret) before the network
	// verify, so a re-run after a transient verify failure keeps the entry.
	if ctxName != "" {
		cf, err := app.LoadContextFile()
		if err != nil {
			_, _ = fmt.Fprintln(s.err, "  ✗ load config:", err)
			return 1
		}
		if other := cf.CollidingContext(ctxName); other != "" {
			_, _ = fmt.Fprintf(s.err, "  ✗ context %q collides with existing %q (both map to %s); pick a name differing by more than case or -/_\n", ctxName, other, app.KeyEnvVar(ctxName))
			return 1
		}
		cf.Contexts[ctxName] = app.ContextEntry{AppID: appID, InstallationID: instID}
		if err := app.SaveContextFile(cf); err != nil {
			_, _ = fmt.Fprintln(s.err, "  ✗ save config:", err)
			return 1
		}
		_, _ = fmt.Fprintf(s.out, "  ✓ wrote context %q to %s\n", ctxName, app.ConfigPath())
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
	service := keychainService(ctxName)
	keyRef := keyPath
	if runtime.GOOS == "darwin" {
		_, _ = fmt.Fprintln(s.out, "  Keychain storage avoids leaving the .pem on disk in plaintext.")
		choice := s.prompt("Save key to macOS keychain? [Y/n]")
		if choice == "" || strings.EqualFold(choice, "y") || strings.EqualFold(choice, "yes") {
			if err := saveToKeychain(service, keyData); err != nil {
				_, _ = fmt.Fprintln(s.err, "  ✗ keychain save failed:", err)
				_, _ = fmt.Fprintln(s.out, "  Falling back to file path:", keyPath)
			} else {
				_, _ = fmt.Fprintf(s.out, "  ✓ saved to keychain (service=%s, account=default)\n", service)
				_, _ = fmt.Fprintln(s.out, "  You can now safely delete the .pem file from disk.")
				keyRef = keychainServiceRef(service)
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

	// Lines to add to the rc, and the block id that keys the managed block
	// (so several contexts can coexist without clobbering each other).
	var rcLines []string
	blockID := "default"
	verify := "gh as-bot doctor"
	if ctxName != "" {
		// Per-context: only the key lives in env (app/installation ids are in
		// the config file). We deliberately do NOT export GH_AS_BOT_CONTEXT —
		// selection is per call via --context, and the team's guard hook
		// ignores an ambient default anyway.
		blockID = ctxName
		keyIsSub := strings.HasPrefix(keyRef, "$(")
		rcLines = []string{
			fmt.Sprintf("# gh-as-bot context %q — pass --context %s per call", ctxName, ctxName),
			shell.render(app.KeyEnvVar(ctxName), keyRef, keyIsSub),
		}
		verify = "gh as-bot --context " + ctxName + " doctor"
	} else {
		rcLines = shell.exportLines(appID, instID, keyRef)
	}

	// Offer to write the block directly so devs don't copy-paste. $SHELL can
	// lie, so confirm the path (Enter accepts, a path overrides, n skips).
	rcPath := shell.rcPath
	promptLabel := fmt.Sprintf("Append to %s? [Y/n] (Enter=yes, or type a path)", shell.rcDescription)
	if rcPath == "" {
		promptLabel = "Path to your shell profile to update (blank to skip)"
	}
	switch ans := s.prompt(promptLabel); {
	case strings.EqualFold(ans, "n") || strings.EqualFold(ans, "no"):
		rcPath = ""
	case ans == "":
		// keep detected rcPath (may be "" for unknown shell → skip)
	default:
		rcPath = expandTilde(ans)
	}

	if rcPath != "" {
		if err := writeManagedRcBlock(rcPath, blockID, rcLines); err != nil {
			_, _ = fmt.Fprintln(s.err, "  ✗ couldn't update "+rcPath+":", err)
			_, _ = fmt.Fprintln(s.out, "  Add these lines manually:")
			for _, line := range rcLines {
				_, _ = fmt.Fprintf(s.out, "    %s\n", line)
			}
		} else {
			_, _ = fmt.Fprintf(s.out, "  ✓ wrote gh-as-bot block to %s\n", rcPath)
		}
	} else {
		_, _ = fmt.Fprintf(s.out, "  Add these to your shell profile (%s):\n\n", shell.rcDescription)
		for _, line := range rcLines {
			_, _ = fmt.Fprintf(s.out, "    %s\n", line)
		}
	}

	_, _ = fmt.Fprintln(s.out, "")
	_, _ = fmt.Fprintf(s.out, "  Open a fresh shell (or `source` your profile), then verify:\n    %s\n", verify)
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

// encodeKeyForKeychain is the single encoder for the keychain write path —
// the production code and its test both go through this, so the test can't
// pass while the real path regresses. base64 collapses a multi-line PEM to
// one newline-free line (Go's StdEncoding never wraps), which is what makes
// the round-trip work; see saveToKeychain.
func encodeKeyForKeychain(pem []byte) string {
	return base64.StdEncoding.EncodeToString(pem)
}

// saveToKeychain stores the PEM under the given keychain service,
// account=default. `-U` upserts, so re-running setup overwrites stale keys
// cleanly. (keychainService / keychainServiceRef live in context.go.)
//
// The PEM is base64-encoded before storage. A raw multi-line PEM is a
// trap: macOS `security ... -w` hex-encodes any value containing a
// newline on read, so a raw PEM round-trips to a hex blob that LoadConfig
// then mistakes for a file path. base64 collapses the key to a single
// newline-free line, which `-w` returns verbatim; keychainServiceRef
// decodes it with the pinned `/usr/bin/base64 -D`.
//
// The encoded key is passed as an argv element to `security`. That is
// briefly visible via `ps` to other local users (CWE-214); we accept it:
// this is a single-user dev tool, `security` has no scriptable stdin-password
// mode, and the alternative (a Cgo keychain library) would break the
// project's zero-dependency guarantee.
func saveToKeychain(service string, pem []byte) error {
	encoded := encodeKeyForKeychain(pem)
	cmd := exec.Command("security", "add-generic-password",
		"-s", service,
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
