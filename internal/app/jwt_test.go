package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func newTestKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestMintAppJWT_StructureAndClaims(t *testing.T) {
	pemBytes := newTestKeyPEM(t)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	jwt, err := MintAppJWT("12345", pemBytes, now)
	if err != nil {
		t.Fatalf("MintAppJWT: %v", err)
	}

	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}

	claimsJSON := decodeSegment(t, parts[1])
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims["iss"] != "12345" {
		t.Errorf("iss = %v, want 12345", claims["iss"])
	}
	// iat must be backdated; exp must be < 10min from now (GitHub cap).
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if iat >= now.Unix() {
		t.Errorf("iat = %d, want < %d (backdated)", iat, now.Unix())
	}
	if exp-now.Unix() >= 600 {
		t.Errorf("exp window = %ds, must be < 600s (GitHub max)", exp-now.Unix())
	}
}

func TestMintAppJWT_PKCS1Key(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	if _, err := MintAppJWT("1", pemBytes, time.Now()); err != nil {
		t.Fatalf("MintAppJWT with PKCS1 key: %v", err)
	}
}

func TestMintAppJWT_RejectsNonPEM(t *testing.T) {
	_, err := MintAppJWT("1", []byte("not a pem"), time.Now())
	if err == nil {
		t.Fatal("expected error on non-PEM input")
	}
}

func decodeSegment(t *testing.T, seg string) []byte {
	t.Helper()
	// MintAppJWT uses base64URL without padding; decode the same way.
	b, err := base64URLDecode(seg)
	if err != nil {
		t.Fatalf("decode segment: %v", err)
	}
	return b
}
