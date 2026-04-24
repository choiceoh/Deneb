package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// pruneLocked applies the retention policy. Caller MUST hold m.mu.
//
//  1. Per-file keep-N: oldest snapshots above retentionN per path are removed.
//  2. Global byte cap: if session total exceeds maxBytes, oldest snapshots
//     across all files are removed until the session fits.
//
// Pruned records are dropped from the index and their blob files deleted.
// Tombstone records count toward retention but contribute zero bytes.
func (m *Manager) pruneLocked() error {
	all, err := readIndex(m.indexPath())
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return nil
	}

	keep := make(map[string]bool, len(all))
	for _, s := range all {
		keep[s.ID] = true
	}

	// Pass 1: per-file keep-N.
	byPath := make(map[string][]*Snapshot)
	for _, s := range all {
		byPath[s.Path] = append(byPath[s.Path], s)
	}
	for _, group := range byPath {
		sort.SliceStable(group, func(i, j int) bool { return group[i].Seq > group[j].Seq })
		for i, s := range group {
			if i >= m.retentionN {
				keep[s.ID] = false
			}
		}
	}

	// Pass 2: global byte cap. Walk oldest-first, drop until total fits.
	// Always protect the most recent surviving snapshot per path so a tight
	// cap doesn't wipe the ability to roll back at all.
	var total int64
	for _, s := range all {
		if keep[s.ID] {
			total += s.Size
		}
	}
	if total > m.maxBytes {
		// Compute the newest-per-path set among keeps.
		newestPerPath := make(map[string]int)
		for _, s := range all {
			if !keep[s.ID] {
				continue
			}
			if seq, ok := newestPerPath[s.Path]; !ok || s.Seq > seq {
				newestPerPath[s.Path] = s.Seq
			}
		}
		ordered := make([]*Snapshot, 0, len(all))
		for _, s := range all {
			if keep[s.ID] {
				ordered = append(ordered, s)
			}
		}
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
		for _, s := range ordered {
			if total <= m.maxBytes {
				break
			}
			if newestPerPath[s.Path] == s.Seq {
				continue // protect the newest snapshot for each path
			}
			keep[s.ID] = false
			total -= s.Size
		}
	}

	// Nothing to prune?
	remaining := make([]*Snapshot, 0, len(all))
	var removed []*Snapshot
	for _, s := range all {
		if keep[s.ID] {
			remaining = append(remaining, s)
		} else {
			removed = append(removed, s)
		}
	}
	if len(removed) == 0 {
		return nil
	}

	// Best-effort: delete blob files (and their atomicfile .lock sidecar)
	// first, then rewrite index.
	var firstBlobErr error
	for _, s := range removed {
		if s.BlobPath == "" {
			continue
		}
		if err := os.Remove(s.BlobPath); err != nil && !errors.Is(err, os.ErrNotExist) && firstBlobErr == nil {
			firstBlobErr = fmt.Errorf("checkpoint: remove blob %s: %w", s.BlobPath, err)
		}
		// Silently best-effort: remove the sidecar lock file left by atomicfile.
		_ = os.Remove(s.BlobPath + ".lock")
	}
	if err := rewriteIndex(m.indexPath(), remaining); err != nil {
		return err
	}
	return firstBlobErr
}
