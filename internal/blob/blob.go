// Package blob stores file contents by SHA-256. Only a local file system
// store ships; the interface leaves room for others.
package blob

import (
	"context"
	"errors"
	"hash/fnv"
	"io"
	"sync"
)

// ErrTooLarge is returned by Put when the content exceeds the limit.
var ErrTooLarge = errors.New("blob: content too large")

// Staged is content written aside: Commit moves it into the store under its
// hash (keeping an existing copy), Discard drops it.
type Staged struct {
	Sum  []byte
	Size int64
	// Head is up to the first 512 bytes, for content type detection.
	Head    []byte
	Commit  func() error
	Discard func()
}

// Store keeps contents by hash.
type Store interface {
	// Put reads r (at most limit bytes, else ErrTooLarge) into a staging area.
	Put(ctx context.Context, r io.Reader, limit int64) (Staged, error)
	Open(ctx context.Context, sum []byte) (io.ReadSeekCloser, error)
	// Delete removes the content; a missing one is not an error.
	Delete(ctx context.Context, sum []byte) error
}

// Locks serializes work on one hash (upload vs cleanup) inside this process.
type Locks struct {
	stripes [64]sync.Mutex
}

// Lock locks the stripe of sum and returns its unlock.
func (l *Locks) Lock(sum []byte) func() {
	h := fnv.New32a()
	_, _ = h.Write(sum)
	m := &l.stripes[h.Sum32()%uint32(len(l.stripes))]
	m.Lock()
	return m.Unlock
}
