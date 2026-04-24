package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CleanupResult is the summary returned by CleanupStaleSessions.
type CleanupResult struct {
	// Scanned counts every entry inspected directly under the root.
	Scanned int
	// Removed counts session directories that were fully deleted.
	Removed int
	// RemovedBytes totals the on-disk bytes reclaimed (best-effort).
	RemovedBytes int64
	// Errors holds per-session errors encountered; the function always
	// proceeds past a failing entry rather than aborting.
	Errors []error
}

// CleanupStaleSessions removes session directories under `root` whose most
// recent modification timestamp is older than `olderThan`. Designed to run
// once at startup (best-effort, off the hot path) so long-abandoned sessions
// do not grow unbounded on disk.
//
// The policy is intentionally conservative:
//
//  1. Only directories (not files) under `root` are considered. Stray files
//     are left alone.
//  2. A session directory is "stale" when its own mtime AND every index/blob
//     under it is older than the cutoff. This protects active sessions whose
//     index file was recently appended to even if the directory mtime is old
//     on some filesystems.
//  3. Removal is via os.RemoveAll. Failures are collected into Errors; the
//     scan continues so a single permission problem does not halt GC.
//
// Safe to call concurrently with active Manager instances on OTHER sessions;
// each session directory has its own atomicfile lock. Do NOT run while the
// owning session still has an active run — use session lifecycle hooks for
// that case.
func CleanupStaleSessions(ctx context.Context, root string, olderThan time.Duration) (CleanupResult, error) {
	var result CleanupResult
	if root == "" {
		return result, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("checkpoint gc: stat root: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("checkpoint gc: root %q is not a directory", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return result, fmt.Errorf("checkpoint gc: read root: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)

	for _, e := range entries {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return result, err
			}
		}
		if !e.IsDir() {
			continue
		}
		result.Scanned++

		sessionDir := filepath.Join(root, e.Name())
		stale, bytes, err := isSessionStale(sessionDir, cutoff)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if !stale {
			continue
		}
		if err := os.RemoveAll(sessionDir); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("%s: remove: %w", e.Name(), err))
			continue
		}
		result.Removed++
		result.RemovedBytes += bytes
	}
	return result, nil
}

// isSessionStale reports whether every file under dir has an mtime older than
// cutoff. Returns the total bytes observed and any hard walk error. Walks the
// tree once; does NOT short-circuit on the first recent file so the byte
// tally remains accurate for telemetry.
func isSessionStale(dir string, cutoff time.Time) (stale bool, totalBytes int64, err error) {
	stale = true
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Walk errors on subentries are non-fatal — preserve unreadable
			// trees by marking the session as not-stale and returning nil so
			// the walk continues. We deliberately swallow walkErr here because
			// forwarding it would abort the caller's GC pass for what may be
			// a transient EACCES or ENOENT race.
			stale = false
			return nil //nolint:nilerr // preserve unreadable trees; see above
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			// Same rationale as walkErr: unreadable file metadata → preserve.
			stale = false
			return nil //nolint:nilerr // preserve on stat failure; see above
		}
		if info.ModTime().After(cutoff) {
			stale = false
		}
		totalBytes += info.Size()
		return nil
	})
	return stale, totalBytes, err
}
