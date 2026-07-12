package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

type retentionSelection struct {
	keep map[string]bool
}

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

	remaining, removed := selectSnapshotsForRetention(all, m.retentionN, m.maxBytes)
	if len(removed) == 0 {
		return nil
	}

	// Blob deletion remains best-effort, but the index must still be rewritten
	// so a single undeletable blob cannot retain every expired checkpoint.
	blobErr := deleteRetiredSnapshotBlobs(removed)
	if err := rewriteIndex(m.indexPath(), remaining); err != nil {
		return err
	}
	return blobErr
}

func selectSnapshotsForRetention(all []*Snapshot, retentionN int, maxBytes int64) ([]*Snapshot, []*Snapshot) {
	selection := newRetentionSelection(all)
	selection.dropBeyondPerPathLimit(all, retentionN)
	selection.dropToByteLimit(all, maxBytes)
	return selection.partition(all)
}

func newRetentionSelection(all []*Snapshot) *retentionSelection {
	keep := make(map[string]bool, len(all))
	for _, s := range all {
		keep[s.ID] = true
	}
	return &retentionSelection{keep: keep}
}

func (s *retentionSelection) dropBeyondPerPathLimit(all []*Snapshot, retentionN int) {
	byPath := make(map[string][]*Snapshot)
	for _, snapshot := range all {
		byPath[snapshot.Path] = append(byPath[snapshot.Path], snapshot)
	}
	for _, group := range byPath {
		sort.SliceStable(group, func(i, j int) bool { return group[i].Seq > group[j].Seq })
		for i, snapshot := range group {
			if i >= retentionN {
				s.keep[snapshot.ID] = false
			}
		}
	}
}

func (s *retentionSelection) dropToByteLimit(all []*Snapshot, maxBytes int64) {
	total := s.retainedBytes(all)
	if total <= maxBytes {
		return
	}

	newestPerPath := s.newestRetainedSequenceByPath(all)
	for _, snapshot := range s.oldestRetainedSnapshots(all) {
		if total <= maxBytes {
			break
		}
		if newestPerPath[snapshot.Path] == snapshot.Seq {
			continue
		}
		s.keep[snapshot.ID] = false
		total -= snapshot.Size
	}
}

func (s *retentionSelection) retainedBytes(all []*Snapshot) int64 {
	var total int64
	for _, snapshot := range all {
		if s.keep[snapshot.ID] {
			total += snapshot.Size
		}
	}
	return total
}

func (s *retentionSelection) newestRetainedSequenceByPath(all []*Snapshot) map[string]int {
	newest := make(map[string]int)
	for _, snapshot := range all {
		if !s.keep[snapshot.ID] {
			continue
		}
		if seq, ok := newest[snapshot.Path]; !ok || snapshot.Seq > seq {
			newest[snapshot.Path] = snapshot.Seq
		}
	}
	return newest
}

func (s *retentionSelection) oldestRetainedSnapshots(all []*Snapshot) []*Snapshot {
	ordered := make([]*Snapshot, 0, len(all))
	for _, snapshot := range all {
		if s.keep[snapshot.ID] {
			ordered = append(ordered, snapshot)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	return ordered
}

func (s *retentionSelection) partition(all []*Snapshot) ([]*Snapshot, []*Snapshot) {
	remaining := make([]*Snapshot, 0, len(all))
	removed := make([]*Snapshot, 0, len(all))
	for _, snapshot := range all {
		if s.keep[snapshot.ID] {
			remaining = append(remaining, snapshot)
		} else {
			removed = append(removed, snapshot)
		}
	}
	return remaining, removed
}

func deleteRetiredSnapshotBlobs(removed []*Snapshot) error {
	var firstBlobErr error
	for _, snapshot := range removed {
		if snapshot.BlobPath == "" {
			continue
		}
		if err := os.Remove(snapshot.BlobPath); err != nil && !errors.Is(err, os.ErrNotExist) && firstBlobErr == nil {
			firstBlobErr = fmt.Errorf("checkpoint: remove blob %s: %w", snapshot.BlobPath, err)
		}
		// Silently best-effort: remove the sidecar lock file left by atomicfile.
		_ = os.Remove(snapshot.BlobPath + ".lock")
	}
	return firstBlobErr
}
