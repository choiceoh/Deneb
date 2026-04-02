// Package atomicfile provides concurrency-safe file writes using
// flock + tmp-file + atomic-rename.  Multiple processes or agents
// writing to the same path are serialised through an advisory lock,
// and the rename ensures readers always see a complete file.
package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"syscall"
)

// Options controls optional behaviours of WriteFile.
type Options struct {
	// Perm is the permission bits for the final file (default 0o644).
	Perm os.FileMode
	// DirPerm is the permission bits for auto-created parent dirs (default 0o755).
	DirPerm os.FileMode
	// Fsync forces an fsync on the temp file before rename for durability.
	Fsync bool
	// Backup creates a .bak copy of the existing file before overwrite.
	Backup bool
}

func (o *Options) perm() os.FileMode {
	if o != nil && o.Perm != 0 {
		return o.Perm
	}
	return 0o644
}

func (o *Options) dirPerm() os.FileMode {
	if o != nil && o.DirPerm != 0 {
		return o.DirPerm
	}
	return 0o755
}

// WriteFile atomically writes data to path with advisory file locking.
//
// Sequence: mkdir parents → flock(path.lock) → write(tmp) → [fsync] → [backup] → rename(tmp, path) → unlock.
// The lock file (path + ".lock") is separate from the data file so
// readers are never blocked and the rename stays atomic.
func WriteFile(path string, data []byte, opts *Options) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, opts.dirPerm()); err != nil {
		return fmt.Errorf("atomicfile: mkdir %s: %w", dir, err)
	}

	// Advisory lock on a sidecar .lock file.
	lockPath := path + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("atomicfile: open lock %s: %w", lockPath, err)
	}
	defer lockFd.Close()

	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("atomicfile: flock %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(lockFd.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Write to a temp file in the same directory (same filesystem → atomic rename).
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	tmp := fmt.Sprintf("%s.%d.%s.tmp", path, os.Getpid(), hex.EncodeToString(randBytes))

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, opts.perm())
	if err != nil {
		return fmt.Errorf("atomicfile: create temp %s: %w", tmp, err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("atomicfile: write temp: %w", err)
	}

	if opts != nil && opts.Fsync {
		if err := f.Sync(); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("atomicfile: fsync temp: %w", err)
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomicfile: close temp: %w", err)
	}

	// Best-effort backup of existing file.
	if opts != nil && opts.Backup {
		if _, statErr := os.Stat(path); statErr == nil {
			_ = copyFile(path, path+".bak")
		}
	}

	// Atomic rename (POSIX guarantees atomicity on same filesystem).
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomicfile: rename: %w", err)
	}

	return nil
}

// copyFile is a best-effort helper for backup creation.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
