// watcher.go — Config hot-reload via polling.
//
// Periodically checks the config file hash and invokes a callback when changes
// are detected. Uses polling instead of fsnotify to avoid an external dependency.
// Debounces rapid changes to prevent thrashing.
package config

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultWatchInterval is how often the watcher checks for config changes.
const DefaultWatchInterval = 5 * time.Second

// DefaultDebounceInterval prevents rapid reload cycles.
const DefaultDebounceInterval = 2 * time.Second

// ReloadCallback is called when a config change is detected.
// Receives the old and new snapshots. Return an error to log a warning.
type ReloadCallback func(old, updated *ConfigSnapshot) error

// Watcher monitors a config file for changes and triggers reload callbacks.
type Watcher struct {
	mu           sync.Mutex
	path         string
	interval     time.Duration
	debounce     time.Duration
	lastHash     string
	lastReloadAt time.Time
	callbacks    []ReloadCallback
	logger       *slog.Logger
	lastSnap     *ConfigSnapshot
}

// NewWatcher creates a config file watcher.
func NewWatcher(path string, logger *slog.Logger) *Watcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		path:     path,
		interval: DefaultWatchInterval,
		debounce: DefaultDebounceInterval,
		logger:   logger.With("pkg", "config-watcher"),
	}
}

// OnReload registers a callback for config changes.
func (w *Watcher) OnReload(cb ReloadCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, cb)
}

// SetInitialHash sets the hash from the initial config load so the watcher
// knows the baseline and doesn't trigger on first poll.
func (w *Watcher) SetInitialHash(hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastHash = hash
}

// SetInitialSnapshot stores the initial config snapshot for diff comparison.
func (w *Watcher) SetInitialSnapshot(snap *ConfigSnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastSnap = snap
	if snap != nil {
		w.lastHash = snap.Hash
	}
}

// Start begins polling the config file. Blocks until ctx is canceled.
func (w *Watcher) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.logger.Info("config watcher started", "path", w.path, "interval", w.interval)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("config watcher stopped")
			return
		case <-ticker.C:
			w.check()
		}
	}
}

// check polls the config file and fires callbacks if changed.
func (w *Watcher) check() {
	w.mu.Lock()
	lastHash := w.lastHash
	lastReloadAt := w.lastReloadAt
	w.mu.Unlock()

	// Debounce: skip if we reloaded very recently.
	if !lastReloadAt.IsZero() && time.Since(lastReloadAt) < w.debounce {
		return
	}

	snap, err := LoadConfig(w.path)
	if err != nil {
		w.logger.Warn("config watcher: failed to load config", "error", err)
		return
	}

	// No change.
	if snap.Hash == lastHash {
		return
	}

	w.logger.Info("config change detected",
		"old_hash", lastHash[:12],
		"new_hash", snap.Hash[:12],
		"valid", snap.Valid,
	)

	if !snap.Valid {
		w.logger.Warn("config change has validation issues, skipping reload",
			"issues", snap.Issues,
		)
		return
	}

	// Fire callbacks.
	w.mu.Lock()
	callbacks := make([]ReloadCallback, len(w.callbacks))
	copy(callbacks, w.callbacks)
	oldSnap := w.lastSnap
	w.lastHash = snap.Hash
	w.lastSnap = snap
	w.lastReloadAt = time.Now()
	w.mu.Unlock()

	for _, cb := range callbacks {
		if err := cb(oldSnap, snap); err != nil {
			w.logger.Warn("config reload callback failed", "error", err)
		}
	}
}
