package skills

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SkillsChangeEvent represents a filesystem change event for skills.
type SkillsChangeEvent struct {
	WorkspaceDir string `json:"workspaceDir,omitempty"`
	Reason       string `json:"reason"`
	ChangedPath  string `json:"changedPath,omitempty"`
}

// ChangeListener is a callback for skill change events.
type ChangeListener func(SkillsChangeEvent)

// defaultSkillsWatchIgnored lists directories excluded from watching.
var defaultSkillsWatchIgnored = map[string]bool{
	".git": true, "node_modules": true, "dist": true,
	".venv": true, "venv": true, "__pycache__": true,
	".mypy_cache": true, ".pytest_cache": true,
	"build": true, ".cache": true,
}

// Watcher manages file system watching and version tracking for skills.
type Watcher struct {
	mu sync.RWMutex

	listeners   map[uint64]ChangeListener
	listenerSeq atomic.Uint64
	listenersMu sync.RWMutex

	workspaceVersions map[string]int64
	versionsMu        sync.RWMutex
	globalVersion     int64

	activeWatchers map[string]*watchState
	watchersMu     sync.Mutex

	logger *slog.Logger
}

type watchState struct {
	stop        chan struct{}
	debounceMs  int
	pathsKey    string
	pendingPath string
	timer       *time.Timer
	mu          sync.Mutex
}

// NewWatcher creates a new skills watcher.
func NewWatcher(logger *slog.Logger) *Watcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Watcher{
		listeners:         make(map[uint64]ChangeListener),
		workspaceVersions: make(map[string]int64),
		activeWatchers:    make(map[string]*watchState),
		logger:            logger,
	}
}

// RegisterChangeListener registers a callback for skill change events.
// Returns an unregister function.
func (w *Watcher) RegisterChangeListener(listener ChangeListener) func() {
	id := w.listenerSeq.Add(1)
	w.listenersMu.Lock()
	w.listeners[id] = listener
	w.listenersMu.Unlock()
	return func() {
		w.listenersMu.Lock()
		delete(w.listeners, id)
		w.listenersMu.Unlock()
	}
}

// BumpVersion increments the version counter and emits a change event.
func (w *Watcher) BumpVersion(workspaceDir, reason, changedPath string) int64 {
	if reason == "" {
		reason = "manual"
	}

	var newVersion int64
	if workspaceDir != "" {
		w.versionsMu.Lock()
		current := w.workspaceVersions[workspaceDir]
		newVersion = bumpVersion(current)
		w.workspaceVersions[workspaceDir] = newVersion
		w.versionsMu.Unlock()
	} else {
		w.versionsMu.Lock()
		w.globalVersion = bumpVersion(w.globalVersion)
		newVersion = w.globalVersion
		w.versionsMu.Unlock()
	}

	w.emit(SkillsChangeEvent{
		WorkspaceDir: workspaceDir,
		Reason:       reason,
		ChangedPath:  changedPath,
	})

	return newVersion
}

// GetVersion returns the current version for a workspace or global.
func (w *Watcher) GetVersion(workspaceDir string) int64 {
	w.versionsMu.RLock()
	defer w.versionsMu.RUnlock()
	if workspaceDir == "" {
		return w.globalVersion
	}
	wv := w.workspaceVersions[workspaceDir]
	if w.globalVersion > wv {
		return w.globalVersion
	}
	return wv
}

// EnsureWatcher starts or updates a file watcher for skill directories.
func (w *Watcher) EnsureWatcher(workspaceDir string, extraDirs []string, debounceMs int) {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if workspaceDir == "" {
		return
	}
	if debounceMs <= 0 {
		debounceMs = 250
	}

	targets := resolveWatchTargets(workspaceDir, extraDirs)
	pathsKey := strings.Join(targets, "|")

	w.watchersMu.Lock()
	defer w.watchersMu.Unlock()

	existing, ok := w.activeWatchers[workspaceDir]
	if ok && existing.pathsKey == pathsKey && existing.debounceMs == debounceMs {
		return // No change needed.
	}

	// Stop old watcher.
	if ok {
		close(existing.stop)
	}

	state := &watchState{
		stop:       make(chan struct{}),
		debounceMs: debounceMs,
		pathsKey:   pathsKey,
	}
	w.activeWatchers[workspaceDir] = state

	// Start polling watcher in background.
	go w.pollSkillFiles(workspaceDir, targets, state)
}

// StopWatcher stops the watcher for a workspace.
func (w *Watcher) StopWatcher(workspaceDir string) {
	w.watchersMu.Lock()
	defer w.watchersMu.Unlock()
	if state, ok := w.activeWatchers[workspaceDir]; ok {
		close(state.stop)
		delete(w.activeWatchers, workspaceDir)
	}
}

// StopAll stops all active watchers.
func (w *Watcher) StopAll() {
	w.watchersMu.Lock()
	defer w.watchersMu.Unlock()
	for key, state := range w.activeWatchers {
		close(state.stop)
		delete(w.activeWatchers, key)
	}
}

// pollSkillFiles polls for SKILL.md file changes using stat-based detection.
// Uses a simple polling approach since fsnotify is not in the Go gateway deps.
func (w *Watcher) pollSkillFiles(workspaceDir string, targets []string, state *watchState) {
	known := make(map[string]time.Time) // path -> modTime
	ticker := time.NewTicker(time.Duration(state.debounceMs*4) * time.Millisecond)
	defer ticker.Stop()

	// Initial scan.
	for _, dir := range targets {
		scanSkillFiles(dir, known)
	}

	for {
		select {
		case <-state.stop:
			return
		case <-ticker.C:
			changed := false
			for _, dir := range targets {
				if detectChanges(dir, known) {
					changed = true
				}
			}
			if changed {
				w.BumpVersion(workspaceDir, "watch", "")
			}
		}
	}
}

// scanSkillFiles finds all SKILL.md files in a directory (up to 2 levels deep
// to support both flat and nested category layouts).
func scanSkillFiles(dir string, known map[string]time.Time) {
	// Check dir/SKILL.md
	checkFile(filepath.Join(dir, "SKILL.md"), known)

	// Check dir/*/SKILL.md (flat) and dir/*/*/SKILL.md (nested category)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || defaultSkillsWatchIgnored[entry.Name()] {
			continue
		}
		childDir := filepath.Join(dir, entry.Name())
		checkFile(filepath.Join(childDir, "SKILL.md"), known)

		// Nested category: dir/category/skill/SKILL.md
		subEntries, err := os.ReadDir(childDir)
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if !sub.IsDir() || defaultSkillsWatchIgnored[sub.Name()] {
				continue
			}
			checkFile(filepath.Join(childDir, sub.Name(), "SKILL.md"), known)
		}
	}
}

func checkFile(path string, known map[string]time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	known[path] = info.ModTime()
}

// detectChanges checks for new/modified/deleted SKILL.md files.
func detectChanges(dir string, known map[string]time.Time) bool {
	changed := false
	current := make(map[string]time.Time)

	// Scan current state (flat + nested category layouts).
	path := filepath.Join(dir, "SKILL.md")
	if info, err := os.Stat(path); err == nil {
		current[path] = info.ModTime()
	}

	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || defaultSkillsWatchIgnored[entry.Name()] {
				continue
			}
			childDir := filepath.Join(dir, entry.Name())
			p := filepath.Join(childDir, "SKILL.md")
			if info, err := os.Stat(p); err == nil {
				current[p] = info.ModTime()
			}
			// Nested category: dir/category/skill/SKILL.md
			subEntries, subErr := os.ReadDir(childDir)
			if subErr == nil {
				for _, sub := range subEntries {
					if !sub.IsDir() || defaultSkillsWatchIgnored[sub.Name()] {
						continue
					}
					sp := filepath.Join(childDir, sub.Name(), "SKILL.md")
					if info, err := os.Stat(sp); err == nil {
						current[sp] = info.ModTime()
					}
				}
			}
		}
	}

	// Detect modifications and additions.
	for path, modTime := range current {
		if prev, ok := known[path]; !ok || !prev.Equal(modTime) {
			changed = true
		}
		known[path] = modTime
	}

	// Detect deletions within this dir.
	prefix := dir + string(filepath.Separator)
	for path := range known {
		if strings.HasPrefix(path, prefix) {
			if _, ok := current[path]; !ok {
				delete(known, path)
				changed = true
			}
		}
	}

	return changed
}

// resolveWatchTargets builds the list of directories to watch for skills.
func resolveWatchTargets(workspaceDir string, extraDirs []string) []string {
	seen := make(map[string]bool)
	var targets []string

	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		targets = append(targets, dir)
	}

	add(filepath.Join(workspaceDir, "skills"))
	add(filepath.Join(workspaceDir, ".agents", "skills"))

	// Config directory skills.
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".deneb", "skills"))
		add(filepath.Join(home, ".agents", "skills"))
	}

	for _, dir := range extraDirs {
		add(dir)
	}

	sort.Strings(targets)
	return targets
}

func bumpVersion(current int64) int64 {
	now := time.Now().UnixMilli()
	if now <= current {
		return current + 1
	}
	return now
}

func (w *Watcher) emit(event SkillsChangeEvent) {
	w.listenersMu.RLock()
	listeners := make([]ChangeListener, 0, len(w.listeners))
	for _, l := range w.listeners {
		listeners = append(listeners, l)
	}
	w.listenersMu.RUnlock()

	for _, l := range listeners {
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Warn("skill change listener panic", "error", r)
				}
			}()
			l(event)
		}()
	}
}
