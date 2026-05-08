package cli

import (
	"bufio"
	"context"
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
	fmt.Fprint(s.out, setupHeader)

	// Step 1
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "Step 1/4 — Create the GitHub App")
	fmt.Fprintln(s.out, "  Open this URL (replace YOUR-ORG):")
	fmt.Fprintln(s.out, "    https://github.com/organizations/YOUR-ORG/settings/apps/new")
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "  Required Repository permissions:")
	fmt.Fprintln(s.out, "    - Pull requests:  Read & write   (post reviews / review comments)")
	fmt.Fprintln(s.out, "    - Contents:       Read           (gh needs this for most read paths)")
	fmt.Fprintln(s.out, "    - Issues:         Read & write   (post issue / PR conversation comments)")
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "  Webhook: uncheck \"Active\" — we don't use webhooks.")
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "  After saving the App:")
	fmt.Fprintln(s.out, "    a) Generate a private key (.pem) and download it.")
	fmt.Fprintln(s.out, "    b) Install the App on the org/repos that should accept bot reviews.")
	fmt.Fprintln(s.out, "    c) Note the App ID (App settings header) and installation ID")
	fmt.Fprintln(s.out, "       (visible in the URL after install: .../installations/<id>/...)")
	fmt.Fprintln(s.out, "")

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
		fmt.Fprintln(s.err, "  ✗ couldn't read key file:", err)
		return 1
	}

	// Step 2
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "Step 2/4 — Verify credentials")
	jwt, err := app.MintAppJWT(appID, keyData, time.Now())
	if err != nil {
		fmt.Fprintln(s.err, "  ✗ JWT minting failed:", err)
		fmt.Fprintln(s.err, "    Check that the .pem is the App private key (not an SSH key).")
		return 1
	}
	tok, err := app.MintInstallationToken(context.Background(), nil, "", jwt, instID)
	if err != nil {
		fmt.Fprintln(s.err, "  ✗ token exchange failed:", err)
		fmt.Fprintln(s.err, "    Check the App ID, installation ID, and that the App is installed on at least one repo.")
		return 1
	}
	fmt.Fprintf(s.out, "  ✓ minted installation token (length=%d, expires_at=%s)\n", len(tok.Token), tok.ExpiresAt.Format(time.RFC3339))

	// Step 3
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "Step 3/4 — Store the private key")
	keyRef := keyPath
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(s.out, "  Keychain storage avoids leaving the .pem on disk in plaintext.")
		choice := s.prompt("Save key to macOS keychain? [Y/n]")
		if choice == "" || strings.EqualFold(choice, "y") || strings.EqualFold(choice, "yes") {
			if err := saveToKeychain(keyData); err != nil {
				fmt.Fprintln(s.err, "  ✗ keychain save failed:", err)
				fmt.Fprintln(s.out, "  Falling back to file path:", keyPath)
			} else {
				fmt.Fprintln(s.out, "  ✓ saved to keychain (service=gh-as-bot, account=default)")
				fmt.Fprintln(s.out, "  You can now safely delete the .pem file from disk.")
				keyRef = `$(security find-generic-password -s gh-as-bot -w)`
			}
		} else {
			fmt.Fprintln(s.out, "  Skipped — env will reference the .pem path directly.")
		}
	} else {
		fmt.Fprintln(s.out, "  Keychain storage is macOS-only; on Linux consider direnv or your")
		fmt.Fprintln(s.out, "  secrets manager. Env will reference the .pem path directly.")
	}

	// Step 4
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "Step 4/4 — Shell configuration")
	fmt.Fprintln(s.out, "  Add this to your shell profile (~/.zshrc or ~/.bashrc):")
	fmt.Fprintln(s.out, "")
	fmt.Fprintf(s.out, "    export GH_AS_BOT_APP_ID=%s\n", appID)
	fmt.Fprintf(s.out, "    export GH_AS_BOT_INSTALLATION_ID=%s\n", instID)
	if strings.HasPrefix(keyRef, "$(") {
		fmt.Fprintf(s.out, "    export GH_AS_BOT_PRIVATE_KEY=\"%s\"\n", keyRef)
	} else {
		fmt.Fprintf(s.out, "    export GH_AS_BOT_PRIVATE_KEY=%s\n", keyRef)
	}
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "  Open a fresh shell (or `source` your profile), then verify:")
	fmt.Fprintln(s.out, "    gh as-bot doctor")
	fmt.Fprintln(s.out, "")
	fmt.Fprintln(s.out, "Setup complete. ✨")
	return 0
}

func (s *setup) prompt(label string) string {
	fmt.Fprintf(s.out, "  %s: ", label)
	line, err := s.in.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

func (s *setup) requirePrompt(label string) (string, error) {
	v := s.prompt(label)
	if v == "" {
		fmt.Fprintln(s.err, "  ✗ value required, aborting setup")
		return "", errors.New("empty input")
	}
	return v, nil
}

// saveToKeychain stores the PEM under service=gh-as-bot account=default.
// `-U` upserts, so re-running setup overwrites stale keys cleanly.
func saveToKeychain(pem []byte) error {
	cmd := exec.Command("security", "add-generic-password",
		"-s", "gh-as-bot",
		"-a", "default",
		"-U",
		"-w", string(pem),
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
