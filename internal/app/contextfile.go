package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ContextEntry is one named credential set. It holds only non-secret
// values; the private key is resolved from KeyEnvVar(name) at mint time.
type ContextEntry struct {
	AppID          string `json:"app_id"`
	InstallationID string `json:"installation_id"`
}

// ContextFile is the on-disk config. It never contains secrets and never
// records a "current" context — selection is always explicit per shell,
// so a default can't leak across concurrent sessions.
type ContextFile struct {
	Contexts map[string]ContextEntry `json:"contexts"`
}

var contextNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// ValidateContextName enforces a name shape that maps unambiguously to an
// env var. Must start with a letter; letters/digits/_/- thereafter.
func ValidateContextName(name string) error {
	if !contextNameRE.MatchString(name) {
		return fmt.Errorf("invalid context name %q: must match %s", name, contextNameRE.String())
	}
	return nil
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// KeyEnvVar maps a context name to the env var holding its PEM key:
// "org" -> "GH_AS_BOT_PRIVATE_KEY_ORG", "my-org" -> "..._MY_ORG".
func KeyEnvVar(name string) string {
	suffix := strings.ToUpper(nonAlnum.ReplaceAllString(name, "_"))
	return "GH_AS_BOT_PRIVATE_KEY_" + suffix
}

// CollidingContext returns the name of an existing context (other than
// `name` itself) whose KeyEnvVar maps to the same env var, or "" if none.
// KeyEnvVar upper-cases and folds non-alphanumerics, so distinct, individually
// valid names like "org"/"Org" or "my-org"/"my_org" alias to one key slot —
// which would silently sign one App's id with another App's key. Callers
// reject an add that collides.
func (cf *ContextFile) CollidingContext(name string) string {
	target := KeyEnvVar(name)
	for existing := range cf.Contexts {
		if existing != name && KeyEnvVar(existing) == target {
			return existing
		}
	}
	return ""
}

// SortedNames returns context names sorted, for stable error/list output.
func (cf *ContextFile) SortedNames() []string {
	names := make([]string, 0, len(cf.Contexts))
	for n := range cf.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ConfigPath resolves the config file location:
// $GH_AS_BOT_CONFIG, else $XDG_CONFIG_HOME/gh-as-bot/config.json,
// else ~/.config/gh-as-bot/config.json.
func ConfigPath() string {
	if p := os.Getenv("GH_AS_BOT_CONFIG"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "gh-as-bot", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "gh-as-bot", "config.json")
	}
	return filepath.Join(home, ".config", "gh-as-bot", "config.json")
}

// LoadContextFile reads the config. A missing file is not an error — it
// yields an empty (non-nil) context map so callers fall through to legacy.
func LoadContextFile() (*ContextFile, error) {
	path := ConfigPath()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &ContextFile{Contexts: map[string]ContextEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	// An empty / whitespace-only file is treated like a missing one — an
	// otherwise-valid config that happens to be 0 bytes must not wedge the
	// tool (it would fail JSON parsing and break even legacy mode).
	if len(bytes.TrimSpace(b)) == 0 {
		return &ContextFile{Contexts: map[string]ContextEntry{}}, nil
	}
	var cf ContextFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cf.Contexts == nil {
		cf.Contexts = map[string]ContextEntry{}
	}
	return &cf, nil
}

// SaveContextFile writes the config atomically (temp file + rename in the
// same dir) so a concurrent reader never observes a half-written file.
func SaveContextFile(cf *ContextFile) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op if the rename below succeeds
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
