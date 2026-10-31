package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// LocalFS keeps blobs under dir/blobs/<hex[0:2]>/<hex>; uploads are written
// to dir/tmp first and renamed into place.
type LocalFS struct {
	dir string
}

// NewLocalFS creates dir/blobs and dir/tmp (mode 0700) when missing.
func NewLocalFS(dir string) (*LocalFS, error) {
	for _, d := range []string{dir, filepath.Join(dir, "blobs"), filepath.Join(dir, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("blob: create %s: %w", d, err)
		}
	}
	return &LocalFS{dir: dir}, nil
}

func (s *LocalFS) path(sum []byte) string {
	h := hex.EncodeToString(sum)
	return filepath.Join(s.dir, "blobs", h[:2], h)
}

// Put streams r into a temp file, hashing it on the way.
func (s *LocalFS) Put(_ context.Context, r io.Reader, limit int64) (Staged, error) {
	f, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "upload-*")
	if err != nil {
		return Staged{}, fmt.Errorf("blob: temp file: %w", err)
	}
	tmp := f.Name()
	fail := func(err error) (Staged, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return Staged{}, err
	}
	h := sha256.New()
	head := &headWriter{max: 512}
	n, err := io.Copy(io.MultiWriter(f, h, head), io.LimitReader(r, limit+1))
	if err != nil {
		return fail(fmt.Errorf("blob: write: %w", err))
	}
	if n > limit {
		return fail(ErrTooLarge)
	}
	if err := f.Sync(); err != nil {
		return fail(fmt.Errorf("blob: sync: %w", err))
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return Staged{}, fmt.Errorf("blob: close: %w", err)
	}
	sum := h.Sum(nil)
	return Staged{
		Sum: sum, Size: n, Head: head.buf,
		Commit: func() error {
			dst := s.path(sum)
			if _, err := os.Stat(dst); err == nil {
				// Same hash, same content: keep the stored copy.
				return os.Remove(tmp)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return fmt.Errorf("blob: create dir: %w", err)
			}
			if err := os.Rename(tmp, dst); err != nil {
				return fmt.Errorf("blob: move into place: %w", err)
			}
			return nil
		},
		Discard: func() { _ = os.Remove(tmp) },
	}, nil
}

// Open opens a stored blob; a missing one is fs.ErrNotExist.
func (s *LocalFS) Open(_ context.Context, sum []byte) (io.ReadSeekCloser, error) {
	f, err := os.Open(s.path(sum))
	if err != nil {
		return nil, fmt.Errorf("blob: open: %w", err)
	}
	return f, nil
}

// Delete removes a stored blob.
func (s *LocalFS) Delete(_ context.Context, sum []byte) error {
	if err := os.Remove(s.path(sum)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("blob: delete: %w", err)
	}
	return nil
}

// headWriter keeps the first max bytes written to it.
type headWriter struct {
	buf []byte
	max int
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - len(w.buf); room > 0 {
		w.buf = append(w.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}
