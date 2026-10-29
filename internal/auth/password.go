// Package auth holds credential primitives (password hashing, tokens, API keys)
// and HTTP request authentication.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters: OWASP minimum (19 MiB, 2 passes, 1 lane) keeps memory low.
const (
	argonMemory  = 19456 // KiB
	argonTime    = 2
	argonThreads = 1
	argonSaltLen = 16
	argonKeyLen  = 32

	minPasswordLen = 8
	maxPasswordLen = 256
)

// ErrPasswordLength is returned for passwords outside 8–256 characters.
var ErrPasswordLength = errors.New("password must be 8–256 characters")

// hashSlots bounds concurrent hashing: each hash needs ~19 MiB of RAM.
var hashSlots = make(chan struct{}, 2)

// inFlight / maxInFlight let tests observe the concurrency bound.
var inFlight, maxInFlight atomic.Int32

func derive(pw string, salt []byte, memory, iterations uint32, threads uint8, keyLen uint32) []byte {
	hashSlots <- struct{}{}
	n := inFlight.Add(1)
	for {
		m := maxInFlight.Load()
		if n <= m || maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	defer func() {
		inFlight.Add(-1)
		<-hashSlots
	}()
	return argon2.IDKey([]byte(pw), salt, iterations, memory, threads, keyLen)
}

// HashPassword returns an argon2id PHC string:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash> (base64 raw std).
func HashPassword(pw string) (string, error) {
	if n := utf8.RuneCountInString(pw); n < minPasswordLen || n > maxPasswordLen {
		return "", ErrPasswordLength
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := derive(pw, salt, argonMemory, argonTime, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the PHC string; malformed input is a mismatch.
func VerifyPassword(pw, phc string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	// Refuse parameters far above ours so a tampered hash cannot exhaust memory.
	if memory == 0 || memory > 4*argonMemory || iterations == 0 || iterations > 10 || threads == 0 || threads > 4 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false
	}
	got := derive(pw, salt, memory, iterations, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified for unknown emails so login timing does not reveal accounts.
var dummyHash = func() string {
	h, err := HashPassword("dummy-password-for-timing")
	if err != nil {
		panic(err)
	}
	return h
}()

// DummyVerify burns one password verification.
func DummyVerify() { _ = VerifyPassword("not-the-password", dummyHash) }
