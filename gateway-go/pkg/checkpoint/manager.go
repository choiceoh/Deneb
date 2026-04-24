// Package checkpoint snapshots files before agent modifications so the user
// can roll back arbitrary prior edits within a session, independent of the
// working tree's git state.
//
// Storage layout:
//
//	<root>/<sessionID>/<filePathHash>/<seq>-<unixNanos>.<ext>(.gz)
//	<root>/<sessionID>/index.jsonl  — one record per snapshot
//
// The design is modelled after Hermes' checkpoint_manager.py (shadow-git
// based) but uses a simpler file-by-file blob layout so it can run with no
// external git dependency and snapshot individual file edits atomically via
// pkg/atomicfile.
//
// Lock hierarchy: Manager.mu only. Never held across external callbacks.
// Methods never recurse into each other while holding mu.
package checkpoint

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Defaults.
const (
	DefaultRetentionN = 20
	DefaultMaxBytes   = 200 * 1024 * 1024 // 200 MB per session
)

// Manager owns a session's checkpoint store.
type Manager struct {
	root       string
	sessionID  string
	mu         sync.Mutex
	retentionN int
	maxBytes   int64
	gzip       bool
	seqCtr     atomic.Uint64 // monotonic sequence within session (lifetime of this Manager)
}

// Option customises Manager construction.
type Option func(*Manager)

// WithRetentionN limits snapshots kept per file (must be > 0).
func WithRetentionN(n int) Option {
	return func(m *Manager) {
		if n > 0 {
			m.retentionN = n
		}
	}
}

// WithMaxBytes sets the per-session total byte cap (must be > 0).
func WithMaxBytes(n int64) Option {
	return func(m *Manager) {
		if n > 0 {
			m.maxBytes = n
		}
	}
}

// WithGzip toggles on-disk gzip compression. Default: true.
func WithGzip(on bool) Option {
	return func(m *Manager) { m.gzip = on }
}

// New constructs a Manager rooted at root for the given sessionID.
// The session directory is created lazily on the first Snapshot.
func New(root, sessionID string, opts ...Option) *Manager {
	m := &Manager{
		root:       root,
		sessionID:  sessionID,
		retentionN: DefaultRetentionN,
		maxBytes:   DefaultMaxBytes,
		gzip:       true,
	}
	for _, o := range opts {
		o(m)
	}
	// Seed seqCtr from any existing index so resumed sessions don't collide.
	if recs, err := readIndex(m.indexPath()); err == nil {
		var maxSeq uint64
		for _, r := range recs {
			if r.Seq < 0 {
				continue
			}
			if u := uint64(r.Seq); u > maxSeq {
				maxSeq = u
			}
		}
		m.seqCtr.Store(maxSeq)
	}
	return m
}

// Snapshot captures the current state of path (or a tombstone if it doesn't
// exist) and records it in the session index. Snapshots with identical SHA-256
// content to the previous snapshot for the same path are deduplicated — the
// existing record is returned.
func (m *Manager) Snapshot(ctx context.Context, path, reason string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: resolve path: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Read existing index once per call — file I/O under lock is acceptable
	// because Manager is per-session (single agent).
	existing, _ := readIndex(m.indexPath())

	tombstone := false
	var size int64
	var sum string
	var data []byte
	info, statErr := os.Stat(abs)
	switch {
	case statErr == nil && info.Mode().IsRegular():
		data, err = os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("checkpoint: read %s: %w", abs, err)
		}
		size = int64(len(data))
		h := sha256.Sum256(data)
		sum = hex.EncodeToString(h[:])
	case errors.Is(statErr, os.ErrNotExist):
		tombstone = true
	case statErr != nil:
		return nil, fmt.Errorf("checkpoint: stat %s: %w", abs, statErr)
	default:
		return nil, fmt.Errorf("checkpoint: %s is not a regular file", abs)
	}

	// Deduplicate against the most recent snapshot for this path.
	if prev := lastForPath(existing, abs); prev != nil {
		if prev.Tombstone && tombstone {
			return prev, nil
		}
		if !prev.Tombstone && !tombstone && prev.SHA256 == sum {
			return prev, nil
		}
	}

	pathHash := hashPath(abs)
	// Atomically obtain the next sequence; Add returns uint64, downcast safely
	// for the reasonable lifetime (2^31 snapshots per session is unreachable).
	nextSeq := m.seqCtr.Add(1)
	if nextSeq > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("checkpoint: sequence counter overflow (session too long)")
	}
	seq := int(nextSeq)
	ts := time.Now().UTC()
	snap := &Snapshot{
		ID:        fmt.Sprintf("%s-%d", pathHash, seq),
		Path:      abs,
		Seq:       seq,
		TakenAt:   ts,
		Size:      size,
		SHA256:    sum,
		Reason:    reason,
		Tombstone: tombstone,
		PathHash:  pathHash,
	}

	// Only write a blob if the file existed.
	if !tombstone {
		blobPath := m.blobPath(snap, filepath.Ext(abs))
		if err := m.writeBlob(blobPath, data); err != nil {
			return nil, err
		}
		snap.BlobPath = blobPath
	}

	if err := appendIndex(m.indexPath(), snap); err != nil {
		// Best-effort cleanup of the written blob so the index stays consistent.
		if snap.BlobPath != "" {
			_ = os.Remove(snap.BlobPath)
		}
		return nil, err
	}

	// Prune after write; failures here don't invalidate the snapshot itself.
	if err := m.pruneLocked(); err != nil {
		return snap, fmt.Errorf("checkpoint: snapshot recorded but prune failed: %w", err)
	}
	return snap, nil
}

// List returns snapshots for the given path (most recent first). If path is
// empty, returns all snapshots in the session. limit <= 0 means no limit.
func (m *Manager) List(path string, limit int) ([]*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := readIndex(m.indexPath())
	if err != nil {
		return nil, err
	}
	var filtered []*Snapshot
	if path == "" {
		filtered = all
	} else {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("checkpoint: resolve path: %w", err)
		}
		for _, s := range all {
			if s.Path == abs {
				filtered = append(filtered, s)
			}
		}
	}
	// Sort descending by seq.
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Seq > filtered[j].Seq })
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

// Restore writes the snapshot's content back to its original path atomically.
// Before restoring, the current state is snapshotted (as reason "pre-restore")
// so the operation can itself be rolled back.
func (m *Manager) Restore(ctx context.Context, snapshotID string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	all, err := readIndex(m.indexPath())
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	target := findByID(all, snapshotID)
	if target == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("checkpoint: snapshot %q not found", snapshotID)
	}
	path := target.Path
	m.mu.Unlock()

	// Take a pre-restore snapshot (Snapshot acquires mu itself — must release first).
	if _, err := m.Snapshot(ctx, path, "pre-restore"); err != nil {
		return nil, fmt.Errorf("checkpoint: pre-restore snapshot: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if target.Tombstone {
		// Restore to "file did not exist": remove current.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("checkpoint: remove for tombstone restore: %w", err)
		}
		return target, nil
	}
	data, err := m.readBlob(target.BlobPath)
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read blob: %w", err)
	}
	if err := atomicfile.WriteFile(path, data, &atomicfile.Options{Fsync: true}); err != nil {
		return nil, fmt.Errorf("checkpoint: restore write: %w", err)
	}
	return target, nil
}

// Diff returns a unified diff between the snapshot contents and the current
// file state. Output is a minimal line-oriented unified diff with 3 lines of
// context — suitable for display in the agent UI but not intended to be fed
// back into `patch`.
func (m *Manager) Diff(snapshotID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := readIndex(m.indexPath())
	if err != nil {
		return "", err
	}
	target := findByID(all, snapshotID)
	if target == nil {
		return "", fmt.Errorf("checkpoint: snapshot %q not found", snapshotID)
	}

	var oldLines []string
	if !target.Tombstone {
		data, err := m.readBlob(target.BlobPath)
		if err != nil {
			return "", fmt.Errorf("checkpoint: read blob: %w", err)
		}
		oldLines = splitLines(string(data))
	}

	var newLines []string
	cur, err := os.ReadFile(target.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("checkpoint: read current: %w", err)
	}
	if err == nil {
		newLines = splitLines(string(cur))
	}

	return unifiedDiff(target.Path+"@"+target.ID, target.Path, oldLines, newLines), nil
}

// SessionDir returns the absolute path of the session's checkpoint storage.
func (m *Manager) SessionDir() string {
	return filepath.Join(m.root, sanitizeSession(m.sessionID))
}

// ── internals ──────────────────────────────────────────────────────────────

func (m *Manager) indexPath() string {
	return filepath.Join(m.SessionDir(), "index.jsonl")
}

func (m *Manager) blobPath(s *Snapshot, ext string) string {
	cleanExt := strings.TrimPrefix(ext, ".")
	// Limit ext to avoid pathological filenames.
	if len(cleanExt) > 16 {
		cleanExt = cleanExt[:16]
	}
	if cleanExt == "" {
		cleanExt = "bin"
	}
	name := fmt.Sprintf("%d-%d.%s", s.Seq, s.TakenAt.UnixNano(), cleanExt)
	if m.gzip {
		name += ".gz"
	}
	return filepath.Join(m.SessionDir(), s.PathHash, name)
}

func (m *Manager) writeBlob(path string, data []byte) error {
	if m.gzip {
		var buf strings.Builder
		gw := gzip.NewWriter(&bytesWriter{w: &buf})
		if _, err := gw.Write(data); err != nil {
			return fmt.Errorf("checkpoint: gzip write: %w", err)
		}
		if err := gw.Close(); err != nil {
			return fmt.Errorf("checkpoint: gzip close: %w", err)
		}
		return atomicfile.WriteFile(path, []byte(buf.String()), &atomicfile.Options{Fsync: true, Perm: 0o600})
	}
	return atomicfile.WriteFile(path, data, &atomicfile.Options{Fsync: true, Perm: 0o600})
}

func (m *Manager) readBlob(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304 — path comes from our own index records.
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(path, ".gz") {
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("checkpoint: gzip reader: %w", err)
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}
	return io.ReadAll(f)
}

// bytesWriter adapts strings.Builder for io.Writer used by gzip.
type bytesWriter struct{ w *strings.Builder }

func (b *bytesWriter) Write(p []byte) (int, error) { return b.w.Write(p) }

func hashPath(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// sanitizeSession strips path separators so callers cannot escape the root.
func sanitizeSession(sessionID string) string {
	if sessionID == "" {
		return "default"
	}
	r := strings.NewReplacer("/", "_", "\\", "_", "..", "_", ":", "_")
	cleaned := r.Replace(sessionID)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "default"
	}
	return cleaned
}

func lastForPath(recs []*Snapshot, abs string) *Snapshot {
	var best *Snapshot
	for _, r := range recs {
		if r.Path != abs {
			continue
		}
		if best == nil || r.Seq > best.Seq {
			best = r
		}
	}
	return best
}

func findByID(recs []*Snapshot, id string) *Snapshot {
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Normalise trailing newline so the diff isn't off by one.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// unifiedDiff produces a minimal unified diff with 3 lines of context.
// Implementation is intentionally simple (O(len(a)*len(b))) — checkpoint
// diffs are human previews, not large-codebase comparisons.
func unifiedDiff(fromLabel, toLabel string, a, b []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", fromLabel, toLabel)
	// Compute LCS via dynamic programming (bounded to 10_000 lines total).
	if len(a)+len(b) > 10_000 {
		fmt.Fprintf(&sb, "@@ diff omitted: files too large (%d + %d lines)\n", len(a), len(b))
		return sb.String()
	}
	rows, cols := len(a)+1, len(b)+1
	lcs := make([][]int, rows)
	for i := range lcs {
		lcs[i] = make([]int, cols)
	}
	for i := 1; i < rows; i++ {
		for j := 1; j < cols; j++ {
			switch {
			case a[i-1] == b[j-1]:
				lcs[i][j] = lcs[i-1][j-1] + 1
			case lcs[i-1][j] >= lcs[i][j-1]:
				lcs[i][j] = lcs[i-1][j]
			default:
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}
	// Walk backwards to build the edit script.
	i, j := len(a), len(b)
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1]:
			ops = append(ops, op{' ', a[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]):
			ops = append(ops, op{'+', b[j-1]})
			j--
		default:
			ops = append(ops, op{'-', a[i-1]})
			i--
		}
	}
	// Reverse.
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	// Emit a single compressed hunk — checkpoint diffs are small previews.
	fmt.Fprintf(&sb, "@@ -1,%d +1,%d @@\n", len(a), len(b))
	for _, o := range ops {
		sb.WriteByte(o.kind)
		sb.WriteString(o.text)
		sb.WriteByte('\n')
	}
	return sb.String()
}
