package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMintInstallationToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("Authorization header missing Bearer prefix")
		}
		if r.URL.Path != "/app/installations/42/access_tokens" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(InstallationToken{
			Token:     "ghs_testtoken",
			ExpiresAt: time.Now().Add(time.Hour),
		})
	}))
	defer srv.Close()

	tok, err := MintInstallationToken(context.Background(), srv.Client(), srv.URL, "fake.jwt.value", "42")
	if err != nil {
		t.Fatalf("MintInstallationToken: %v", err)
	}
	if tok.Token != "ghs_testtoken" {
		t.Errorf("token = %q", tok.Token)
	}
}

func TestMintInstallationToken_NonCreatedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	_, err := MintInstallationToken(context.Background(), srv.Client(), srv.URL, "x", "1")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}
