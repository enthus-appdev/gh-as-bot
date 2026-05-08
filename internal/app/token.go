package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InstallationToken is the short-lived (1 hour) GitHub App installation
// access token. The Token field is what gh expects via GH_TOKEN.
type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MintInstallationToken exchanges an App JWT for an installation access
// token. The returned Token authenticates subsequent API calls as the
// installation — i.e., as `<app-name>[bot]` on PR comments and reviews.
//
// The endpoint defaults to api.github.com but can be overridden via
// GITHUB_API_URL for GHES. The HTTP client is injectable for tests.
func MintInstallationToken(ctx context.Context, client *http.Client, apiURL, jwt, installationID string) (*InstallationToken, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", apiURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("github %d: %s", resp.StatusCode, string(body))
	}
	var t InstallationToken
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &t, nil
}

// base64URLDecode is exported only for tests in this package; it mirrors
// the encoder used in jwt.go so segments round-trip cleanly.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
