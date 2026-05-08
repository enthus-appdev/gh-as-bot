package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config carries the GitHub App credentials needed to mint an
// installation access token. It is populated from environment variables
// — never persisted to disk by gh-as-bot itself.
type Config struct {
	AppID          string
	InstallationID string
	PrivateKey     []byte // PEM bytes
}

// LoadConfig resolves App credentials from the environment.
//
// GH_AS_BOT_PRIVATE_KEY accepts either:
//   - a literal PEM string (when the value starts with "-----BEGIN")
//   - a path to a .pem file
//
// Splitting these cases by content avoids forcing callers to pick
// between two env vars when their secrets manager (op, keychain) only
// returns the file contents anyway.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		AppID:          os.Getenv("GH_AS_BOT_APP_ID"),
		InstallationID: os.Getenv("GH_AS_BOT_INSTALLATION_ID"),
	}

	keyValue := os.Getenv("GH_AS_BOT_PRIVATE_KEY")
	if keyValue != "" {
		key, err := loadKey(keyValue)
		if err != nil {
			return nil, fmt.Errorf("load private key: %w", err)
		}
		cfg.PrivateKey = key
	}

	var missing []string
	if cfg.AppID == "" {
		missing = append(missing, "GH_AS_BOT_APP_ID")
	}
	if cfg.InstallationID == "" {
		missing = append(missing, "GH_AS_BOT_INSTALLATION_ID")
	}
	if len(cfg.PrivateKey) == 0 {
		missing = append(missing, "GH_AS_BOT_PRIVATE_KEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func loadKey(value string) ([]byte, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "-----BEGIN") {
		return []byte(value), nil
	}
	b, err := os.ReadFile(value)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("private key file is empty")
	}
	return b, nil
}
