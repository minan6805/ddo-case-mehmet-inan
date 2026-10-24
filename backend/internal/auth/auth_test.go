package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestTokenDigestRejectsMalformedTokens(t *testing.T) {
	for _, token := range []string{"", "not-base64!", "abc", base64.RawURLEncoding.EncodeToString(make([]byte, 31)), base64.RawURLEncoding.EncodeToString(make([]byte, 33))} {
		if _, ok := tokenDigest(token); ok {
			t.Fatalf("accepted malformed token %q", token)
		}
	}
}

func TestTokenDigestHashesRawRandomBytes(t *testing.T) {
	raw := bytes.Repeat([]byte{42}, 32)
	expected := sha256.Sum256(raw)
	digest, ok := tokenDigest(base64.RawURLEncoding.EncodeToString(raw))
	if !ok || !bytes.Equal(digest, expected[:]) {
		t.Fatal("unexpected token digest")
	}
	if bytes.Equal(digest, raw) {
		t.Fatal("raw token stored instead of digest")
	}
}

func TestSessionTTLValidation(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Hour, 30 * time.Second, 8 * 24 * time.Hour} {
		if _, err := New(nil, ttl); err == nil {
			t.Fatalf("accepted TTL %s", ttl)
		}
	}
}
