// WAL (Write-Ahead Log) provides durable session state persistence.
//
// Every session mutation is appended as a single JSON line to a WAL file.
// On startup, the WAL is replayed to reconstruct the in-memory session map.
// Periodic compaction snapshots current state and truncates the log.
package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WAL operation types.
const (
	walOpSet    = "set"
	walOpPatch  = "patch"
	walOpDelete = "delete"
)

// walEntry is a single line in the WAL file.
type walEntry struct {
	Op      string   `json:"op"`
	Session *Session `json:"session,omitempty"`
	Key     string   `json:"key,omitempty"`
	Ts      int64    `json:"ts"`
}

// WAL persists session state changes to disk for crash recovery.
// It subscribes to the session EventBus and appends mutations atomically.
type WAL struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
	path string
	mgr  *Manager

	unsub  func() // EventBus unsubscribe
	logger *slog.Logger

	// compaction state
	compactInterval time.Duration
	stopCompact     chan struct{}
}

// WALConfig configures the session WAL.
type WALConfig struct {
	// Dir is the directory for WAL files. Defaults to ~/.deneb/.
	Dir string
	// CompactInterval is how often to compact the WAL. Zero disables.
	CompactInterval time.Duration
	Logger          *slog.Logger
}

const (
	walFileName          = "sessions.wal"
	walSnapshotFileName  = "sessions.snapshot"
	defaultCompactPeriod = 30 * time.Minute
)

// NewWAL creates a WAL that persists session changes for the given Manager.
// Call Start() to begin writing and replaying.
func NewWAL(mgr *Manager, cfg WALConfig) *WAL {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.CompactInterval == 0 {
		cfg.CompactInterval = defaultCompactPeriod
	}
	return &WAL{
		path:            filepath.Join(cfg.Dir, walFileName),
		mgr:             mgr,
		logger:          cfg.Logger,
		compactInterval: cfg.CompactInterval,
		stopCompact:     make(chan struct{}),
	}
}

// Start replays existing WAL entries, then subscribes to future mutations.
func (w *WAL) Start() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("wal: mkdir: %w", err)
	}

	// Replay snapshot first (if exists), then WAL entries on top.
	snapshotPath := filepath.Join(filepath.Dir(w.path), walSnapshotFileName)
	restored := w.replayFile(snapshotPath)
	restored += w.replayFile(w.path)

	if restored > 0 {
		w.logger.Info("wal: restored sessions", "count", restored)
	}

	// Open WAL for append.
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("wal: open: %w", err)
	}
	w.file = f
	w.enc = json.NewEncoder(f)

	// Subscribe to session events for future mutations.
	w.unsub = w.mgr.EventBusRef().Subscribe(w.onEvent)

	// Start periodic compaction.
	if w.compactInterval > 0 {
		go w.compactLoop()
	}

	return nil
}

// Stop unsubscribes from events, stops compaction, and closes the WAL file.
func (w *WAL) Stop() {
	if w.unsub != nil {
		w.unsub()
		w.unsub = nil
	}
	close(w.stopCompact)

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
}

// onEvent handles session lifecycle events and writes them to the WAL.
func (w *WAL) onEvent(event Event) {
	now := time.Now().UnixMilli()

	switch event.Kind {
	case EventCreated, EventStatusChanged:
		// Snapshot the full session state after mutation.
		s := w.mgr.Get(event.Key)
		if s == nil {
			return
		}
		w.append(walEntry{Op: walOpSet, Session: s, Ts: now})

	case EventDeleted:
		w.append(walEntry{Op: walOpDelete, Key: event.Key, Ts: now})
	}
}

// append writes a single entry to the WAL file.
func (w *WAL) append(entry walEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	if err := w.enc.Encode(entry); err != nil {
		w.logger.Warn("wal: write failed", "op", entry.Op, "error", err)
		return
	}
	// Sync to ensure durability. Single-user system so fsync overhead is fine.
	w.file.Sync()
}

// replayFile reads a WAL or snapshot file and applies entries to the Manager.
// Returns the number of sessions restored.
func (w *WAL) replayFile(path string) int {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			w.logger.Warn("wal: cannot open for replay", "path", path, "error", err)
		}
		return 0
	}
	defer f.Close()

	restored := 0
	scanner := bufio.NewScanner(f)
	// Allow large lines (up to 1MB) for sessions with large LastOutput.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry walEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			w.logger.Warn("wal: skipping corrupt entry", "error", err)
			continue
		}
		switch entry.Op {
		case walOpSet:
			if entry.Session == nil {
				continue
			}
			// Direct set bypasses state machine validation during replay
			// since entries were already validated when originally written.
			w.mgr.replaySet(entry.Session)
			restored++
		case walOpDelete:
			w.mgr.Delete(entry.Key)
		}
	}

	if err := scanner.Err(); err != nil {
		w.logger.Warn("wal: replay scan error", "path", path, "error", err)
	}
	return restored
}

// Compact writes a snapshot of all current sessions, then truncates the WAL.
func (w *WAL) Compact() error {
	snapshotPath := filepath.Join(filepath.Dir(w.path), walSnapshotFileName)
	tmpPath := snapshotPath + ".tmp"

	// Write snapshot to temp file.
	sessions := w.mgr.List()
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("wal: compact create: %w", err)
	}
	enc := json.NewEncoder(f)
	now := time.Now().UnixMilli()
	for _, s := range sessions {
		if err := enc.Encode(walEntry{Op: walOpSet, Session: s, Ts: now}); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("wal: compact encode: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("wal: compact sync: %w", err)
	}
	f.Close()

	// Atomic rename.
	if err := os.Rename(tmpPath, snapshotPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("wal: compact rename: %w", err)
	}

	// Truncate the WAL since snapshot is now authoritative.
	w.mu.Lock()
	if w.file != nil {
		w.file.Close()
	}
	w.file, err = os.Create(w.path)
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("wal: compact truncate: %w", err)
	}
	w.enc = json.NewEncoder(w.file)
	w.mu.Unlock()

	w.logger.Info("wal: compacted", "sessions", len(sessions))
	return nil
}

// compactLoop runs periodic compaction until stopped.
func (w *WAL) compactLoop() {
	ticker := time.NewTicker(w.compactInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCompact:
			return
		case <-ticker.C:
			if err := w.Compact(); err != nil {
				w.logger.Warn("wal: compaction failed", "error", err)
			}
		}
	}
}
