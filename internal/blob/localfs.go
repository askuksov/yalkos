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
	"strings"
	"time"
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

// SweepTemp removes dir/tmp/upload-* files last modified before before; it
// keeps going past a file it cannot remove.
func (s *LocalFS) SweepTemp(_ context.Context, before time.Time) (int, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "tmp"))
	if err != nil {
		return 0, fmt.Errorf("blob: read tmp: %w", err)
	}
	n := 0
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "upload-") {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("blob: stat temp file: %w", err))
			continue
		}
		if !info.ModTime().Before(before) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, "tmp", e.Name())); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, fmt.Errorf("blob: remove temp file: %w", err))
			}
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// Stale returns the sums of blobs under dir/blobs last modified at or after
// from and before before. Files whose name is not a lowercase hex SHA-256 under its own
// prefix directory are not blobs and are skipped.
func (s *LocalFS) Stale(_ context.Context, from, before time.Time) ([][]byte, error) {
	root := filepath.Join(s.dir, "blobs")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("blob: read blobs: %w", err)
	}
	var out [][]byte
	var errs []error
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, d.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("blob: read blobs dir: %w", err))
			continue
		}
		for _, e := range entries {
			name := e.Name()
			sum, err := hex.DecodeString(name)
			if err != nil || len(sum) != sha256.Size || hex.EncodeToString(sum) != name || name[:2] != d.Name() || !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("blob: stat blob: %w", err))
				continue
			}
			if mt := info.ModTime(); !mt.Before(from) && mt.Before(before) {
				out = append(out, sum)
			}
		}
	}
	return out, errors.Join(errs...)
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
