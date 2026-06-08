package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config carries the GitHub App credentials needed to mint an
// installation access token. It is populated from environment variables
// and/or the context config file — the private key is never persisted to
// disk by gh-as-bot itself.
type Config struct {
	AppID          string
	InstallationID string
	PrivateKey     []byte // PEM bytes
}

// LoadConfig resolves credentials with no explicit context flag — used by
// callers that only have env. Equivalent to ResolveConfig("").
func LoadConfig() (*Config, error) {
	return ResolveConfig("")
}

// ResolveConfig picks credentials using mutually-exclusive modes, so the
// precedence is predictable rather than an interleaving of fields:
//
//  1. flagContext != ""             -> that context (error if unknown)
//  2. GH_AS_BOT_CONTEXT set         -> that context (error if unknown)
//  3. contexts defined, none picked -> error (never guess an identity)
//  4. otherwise                     -> legacy env mode (today's behavior)
//
// In context mode the private key comes from the per-context env var
// KeyEnvVar(name); it deliberately does NOT fall back to the bare
// GH_AS_BOT_PRIVATE_KEY, which would sign one App's id with another App's
// key and surface as a confusing 401.
func ResolveConfig(flagContext string) (*Config, error) {
	name := flagContext
	if name == "" {
		name = os.Getenv("GH_AS_BOT_CONTEXT")
	}

	cf, err := LoadContextFile()
	if err != nil {
		return nil, err
	}

	if name != "" {
		entry, ok := cf.Contexts[name]
		if !ok {
			return nil, fmt.Errorf("unknown context %q (available: %s)", name, strings.Join(cf.SortedNames(), ", "))
		}
		// Validate the (cheap) config fields before resolving the key.
		var missing []string
		if entry.AppID == "" {
			missing = append(missing, "app_id")
		}
		if entry.InstallationID == "" {
			missing = append(missing, "installation_id")
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("context %q is incomplete: missing %s", name, strings.Join(missing, ", "))
		}
		keyVar := KeyEnvVar(name)
		keyValue := os.Getenv(keyVar)
		if keyValue == "" {
			return nil, fmt.Errorf("context %q selected but %s is unset", name, keyVar)
		}
		key, err := loadKey(keyValue)
		if err != nil {
			return nil, fmt.Errorf("load private key: %w", err)
		}
		return &Config{AppID: entry.AppID, InstallationID: entry.InstallationID, PrivateKey: key}, nil
	}

	if len(cf.Contexts) > 0 {
		return nil, fmt.Errorf("no context selected; pass --context or set GH_AS_BOT_CONTEXT (available: %s)", strings.Join(cf.SortedNames(), ", "))
	}

	return loadLegacyConfig()
}

// loadLegacyConfig is the original env-only behavior, preserved verbatim:
// GH_AS_BOT_PRIVATE_KEY accepts inline PEM (starts with "-----BEGIN") or a
// path to a .pem file. Missing values are reported by env-var name.
func loadLegacyConfig() (*Config, error) {
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
