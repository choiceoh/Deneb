package checkpoint

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// appendIndex atomically appends a single snapshot record to the session
// index file. Uses O_APPEND so concurrent Manager usage on the same session
// (not expected, but defensible) doesn't lose writes.
func appendIndex(path string, s *Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("checkpoint: mkdir index: %w", err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("checkpoint: marshal index: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304 — path is session-internal.
	if err != nil {
		return fmt.Errorf("checkpoint: open index: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("checkpoint: write index: %w", err)
	}
	return nil
}

// readIndex parses the session's JSONL index. Corrupt trailing lines (e.g.
// from a crash mid-write) are silently skipped so the manager can still
// function after an abrupt shutdown.
func readIndex(path string) ([]*Snapshot, error) {
	f, err := os.Open(path) //nolint:gosec // G304 — path is session-internal.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("checkpoint: open index: %w", err)
	}
	defer f.Close()

	var snaps []*Snapshot
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var s Snapshot
		if err := json.Unmarshal(line, &s); err != nil {
			continue // graceful degradation
		}
		cp := s // avoid re-using loop variable address
		snaps = append(snaps, &cp)
	}
	if err := scanner.Err(); err != nil {
		return snaps, fmt.Errorf("checkpoint: scan index: %w", err)
	}
	// Stable sort by Seq ascending for deterministic callers.
	sort.SliceStable(snaps, func(i, j int) bool { return snaps[i].Seq < snaps[j].Seq })
	return snaps, nil
}

// rewriteIndex atomically replaces the index file with the given records.
// Used by the retention pruner.
func rewriteIndex(path string, recs []*Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("checkpoint: mkdir index: %w", err)
	}
	var buf []byte
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("checkpoint: marshal index rewrite: %w", err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	// Atomic: write to tmp + rename (we'd use atomicfile here but it requires
	// the same directory; index rewrite is rare so a direct tmp-rename is fine).
	tmp := path + ".rewrite.tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("checkpoint: write index tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("checkpoint: rename index: %w", err)
	}
	return nil
}
