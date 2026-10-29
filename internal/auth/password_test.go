package auth

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash %q has wrong prefix", h)
	}
	if !VerifyPassword("correct horse", h) {
		t.Error("right password rejected")
	}
	if VerifyPassword("wrong horse", h) {
		t.Error("wrong password accepted")
	}
}

func TestHashUniqueSalt(t *testing.T) {
	a, _ := HashPassword("same password")
	b, _ := HashPassword("same password")
	if a == b {
		t.Error("two hashes of the same password are equal")
	}
}

func TestPasswordLength(t *testing.T) {
	for _, pw := range []string{strings.Repeat("a", 7), strings.Repeat("a", 257)} {
		if _, err := HashPassword(pw); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("len %d: err = %v, want ErrPasswordLength", len(pw), err)
		}
	}
	for _, pw := range []string{strings.Repeat("a", 8), strings.Repeat("a", 256)} {
		if _, err := HashPassword(pw); err != nil {
			t.Errorf("len %d: err = %v", len(pw), err)
		}
	}
}

func TestVerifyMalformed(t *testing.T) {
	for _, h := range []string{"", "$argon2id$garbage", "$2a$10$abcdefghijklmnopqrstuv", "$argon2id$v=19$m=x,t=2,p=1$aaaa$bbbb"} {
		if VerifyPassword("whatever1", h) {
			t.Errorf("malformed %q accepted", h)
		}
	}
}

func TestHashConcurrencyBounded(t *testing.T) {
	maxInFlight.Store(0)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() { _, _ = HashPassword("parallel password") })
	}
	wg.Wait()
	if got := maxInFlight.Load(); got > 2 || got < 1 {
		t.Errorf("max concurrent hashes = %d, want 1..2", got)
	}
}
