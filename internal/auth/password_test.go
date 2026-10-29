package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword(context.Background(), "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("hash %q has wrong prefix", h)
	}
	if ok, _ := VerifyPassword(context.Background(), "correct horse", h); !ok {
		t.Error("right password rejected")
	}
	if ok, _ := VerifyPassword(context.Background(), "wrong horse", h); ok {
		t.Error("wrong password accepted")
	}
}

func TestHashUniqueSalt(t *testing.T) {
	a, _ := HashPassword(context.Background(), "same password")
	b, _ := HashPassword(context.Background(), "same password")
	if a == b {
		t.Error("two hashes of the same password are equal")
	}
}

func TestPasswordLength(t *testing.T) {
	for _, pw := range []string{strings.Repeat("a", 7), strings.Repeat("a", 257)} {
		if _, err := HashPassword(context.Background(), pw); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("len %d: err = %v, want ErrPasswordLength", len(pw), err)
		}
	}
	for _, pw := range []string{strings.Repeat("a", 8), strings.Repeat("a", 256)} {
		if _, err := HashPassword(context.Background(), pw); err != nil {
			t.Errorf("len %d: err = %v", len(pw), err)
		}
	}
}

func TestVerifyMalformed(t *testing.T) {
	for _, h := range []string{"", "$argon2id$garbage", "$2a$10$abcdefghijklmnopqrstuv", "$argon2id$v=19$m=x,t=2,p=1$aaaa$bbbb"} {
		if ok, _ := VerifyPassword(context.Background(), "whatever1", h); ok {
			t.Errorf("malformed %q accepted", h)
		}
	}
}

func TestHashConcurrencyBounded(t *testing.T) {
	maxInFlight.Store(0)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() { _, _ = HashPassword(context.Background(), "parallel password") })
	}
	wg.Wait()
	if got := maxInFlight.Load(); got > 2 || got < 1 {
		t.Errorf("max concurrent hashes = %d, want 1..2", got)
	}
}

func TestHashWaitIsBounded(t *testing.T) {
	old := slotWait
	slotWait = 50 * time.Millisecond
	defer func() { slotWait = old }()
	hashSlots <- struct{}{}
	hashSlots <- struct{}{}
	defer func() { <-hashSlots; <-hashSlots }()

	start := time.Now()
	if _, err := HashPassword(context.Background(), "some password"); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waited %v, want ≈ slotWait", d)
	}
}

func TestHashRespectsContext(t *testing.T) {
	hashSlots <- struct{}{}
	hashSlots <- struct{}{}
	defer func() { <-hashSlots; <-hashSlots }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyPassword(ctx, "pw", dummyHash); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
