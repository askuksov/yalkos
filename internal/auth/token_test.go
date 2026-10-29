package auth

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"testing"
)

func TestNewAPIKeyFormat(t *testing.T) {
	re := regexp.MustCompile(`^yal_api_[0-9A-Za-z]{40}$`)
	for range 50 {
		plain, hash, prefix := NewAPIKey()
		if !re.MatchString(plain) {
			t.Fatalf("key %q does not match %s", plain, re)
		}
		if prefix != plain[:12]+"…" {
			t.Errorf("display prefix = %q, want %q", prefix, plain[:12]+"…")
		}
		if !bytes.Equal(HashSecret(plain), hash) {
			t.Error("hash != HashSecret(plain)")
		}
	}
}

func TestNewTokenUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		plain, hash := NewToken()
		if seen[plain] {
			t.Fatal("duplicate token")
		}
		seen[plain] = true
		raw, err := base64.RawURLEncoding.DecodeString(plain)
		if err != nil || len(raw) != 32 {
			t.Fatalf("token %q: decoded %d bytes, err %v", plain, len(raw), err)
		}
		if !bytes.Equal(HashSecret(plain), hash) {
			t.Fatal("hash != HashSecret(plain)")
		}
	}
}

func TestParseAuthorization(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"Bearer yal_api_x", "yal_api_x", true},
		{"bearer  k ", "k", true},
		{"k", "k", true},
		{"", "", false},
		{"Bearer ", "", false},
		{"Basic abc", "", false},
	}
	for _, c := range cases {
		got, ok := ParseAuthorization(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseAuthorization(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
