package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const (
	apiKeyPrefix   = "yal_api_"
	apiKeyRandLen  = 40
	displayPrefixN = 12
	base62         = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// HashSecret is the SHA-256 digest stored for high-entropy secrets (tokens, API keys).
func HashSecret(plain string) []byte {
	sum := sha256.Sum256([]byte(plain))
	return sum[:]
}

// NewToken returns a 32-byte random token (base64url, no padding) and its hash.
func NewToken() (plain string, hash []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, HashSecret(plain)
}

// NewAPIKey returns "yal_api_" + 40 base62 chars, its hash and a display prefix.
func NewAPIKey() (plain string, hash []byte, displayPrefix string) {
	var sb strings.Builder
	sb.WriteString(apiKeyPrefix)
	buf := make([]byte, 1)
	for sb.Len() < len(apiKeyPrefix)+apiKeyRandLen {
		_, _ = rand.Read(buf)
		// Rejection sampling: 248 = 4*62, keeps the distribution uniform.
		if buf[0] < 248 {
			sb.WriteByte(base62[int(buf[0])%62])
		}
	}
	plain = sb.String()
	return plain, HashSecret(plain), plain[:displayPrefixN] + "…"
}

// ParseAuthorization extracts the key from "Bearer <key>" or a bare "<key>".
func ParseAuthorization(h string) (key string, ok bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return "", false
	}
	scheme, rest, found := strings.Cut(h, " ")
	if !found {
		// A lone scheme word ("Bearer") is not a key.
		if strings.EqualFold(h, "Bearer") {
			return "", false
		}
		return h, true
	}
	if !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	return rest, rest != ""
}
