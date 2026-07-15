package briefcase

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	ErrRunRootClosed  = errors.New("briefcase run root is closed")
	ErrRunRootClaimed = errors.New("briefcase RunRoot is already claimed; use a distinct RunRoot for every arm")
)

// RunPaths is the complete set of filesystem roots allocated to one run.
// Every path is absolute and created with mode 0700.
type RunPaths struct {
	Root      string
	Home      string
	State     string
	Files     string
	Workspace string
	Artifacts string
	Logs      string
	Temp      string
}

// RunRoot owns a fresh, disposable filesystem tree for a single evaluation.
// It never mutates process-global environment variables; callers pass Environ
// to the component they are isolating.
type RunRoot struct {
	paths RunPaths

	mu      sync.RWMutex
	closed  bool
	claimed bool
}

// NewRunRoot creates a unique run directory below parent. An empty parent uses
// the operating system's default temporary directory. A caller-supplied parent
// must already exist; the function never changes its permissions.
func NewRunRoot(parent string) (*RunRoot, error) {
	if parent != "" {
		abs, err := filepath.Abs(parent)
		if err != nil {
			return nil, fmt.Errorf("briefcase: resolve run parent: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("briefcase: stat run parent: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("briefcase: run parent is not a directory: %s", abs)
		}
		parent = abs
	}

	root, err := os.MkdirTemp(parent, "deneb-briefcase-run-")
	if err != nil {
		return nil, fmt.Errorf("briefcase: create run root: %w", err)
	}
	cleanup := func(cause error) (*RunRoot, error) {
		_ = os.RemoveAll(root)
		return nil, cause
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return cleanup(fmt.Errorf("briefcase: secure run root: %w", err))
	}

	paths := RunPaths{
		Root:      root,
		Home:      filepath.Join(root, "home"),
		State:     filepath.Join(root, "state"),
		Files:     filepath.Join(root, "files"),
		Workspace: filepath.Join(root, "workspace"),
		Artifacts: filepath.Join(root, "artifacts"),
		Logs:      filepath.Join(root, "logs"),
		Temp:      filepath.Join(root, "tmp"),
	}
	dirs := []string{
		paths.Home,
		paths.State,
		paths.Files,
		paths.Workspace,
		paths.Artifacts,
		paths.Logs,
		paths.Temp,
		filepath.Join(paths.Home, ".cache"),
		filepath.Join(paths.Home, ".config"),
		filepath.Join(paths.Home, ".local", "share"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return cleanup(fmt.Errorf("briefcase: create isolated directory %s: %w", dir, err))
		}
		// MkdirAll is affected by the process umask and preserves pre-existing
		// components, so explicitly enforce the contract on every owned path.
		if err := os.Chmod(dir, 0o700); err != nil {
			return cleanup(fmt.Errorf("briefcase: secure isolated directory %s: %w", dir, err))
		}
	}
	if err := writeRunConfig(paths); err != nil {
		return cleanup(err)
	}

	return &RunRoot{paths: paths}, nil
}

func writeRunConfig(paths RunPaths) error {
	// This minimal config makes the production workspace resolver land on the
	// explicit run workspace and disables background sources that could otherwise
	// consult real mail, cron, or LMTP state. The runtime policy remains the
	// enforcement boundary; the config is defense in depth.
	config := map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{"workspace": paths.Workspace},
		},
		"cron":      map[string]any{"enabled": false},
		"gmailPoll": map[string]any{"enabled": false},
		"mailLmtp":  map[string]any{"enabled": false},
		"session":   map[string]any{"autoResume": false},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("briefcase: marshal isolated config: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(paths.State, "deneb.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("briefcase: write isolated config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("briefcase: secure isolated config: %w", err)
	}
	return nil
}

// Paths returns an immutable-by-copy snapshot of the run's path set.
func (r *RunRoot) Paths() (RunPaths, error) {
	if r == nil {
		return RunPaths{}, ErrRunRootClosed
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return RunPaths{}, ErrRunRootClosed
	}
	return r.paths, nil
}

// Environment returns the path-related environment for an isolated Deneb run.
// It is intentionally a map so callers cannot accidentally inherit the parent
// process environment by omission.
func (r *RunRoot) Environment() (map[string]string, error) {
	paths, err := r.Paths()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"HOME":                          paths.Home,
		"DENEB_STATE_DIR":               paths.State,
		"DENEB_FILES_DIR":               paths.Files,
		"DENEB_CONFIG_PATH":             filepath.Join(paths.State, "deneb.json"),
		"DENEB_PROFILE":                 "briefcase",
		"DENEB_REDACT_SECRETS":          "true",
		"DENEB_BRIEFCASE_RUN_ROOT":      paths.Root,
		"DENEB_BRIEFCASE_WORKSPACE_DIR": paths.Workspace,
		"DENEB_BRIEFCASE_ARTIFACTS_DIR": paths.Artifacts,
		"DENEB_BRIEFCASE_LOGS_DIR":      paths.Logs,
		"TMPDIR":                        paths.Temp,
		"XDG_CACHE_HOME":                filepath.Join(paths.Home, ".cache"),
		"XDG_CONFIG_HOME":               filepath.Join(paths.Home, ".config"),
		"XDG_DATA_HOME":                 filepath.Join(paths.Home, ".local", "share"),
	}, nil
}

// Environ overlays the isolated path variables on base. It is useful only when
// a caller has explicitly audited the inherited variables. For the fail-closed
// default, prefer IsolatedEnviron.
func (r *RunRoot) Environ(base []string) ([]string, error) {
	overlay, err := r.Environment()
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(base)+len(overlay))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		values[key] = value
	}
	for key, value := range overlay {
		values[key] = value
	}
	return sortedEnvironment(values), nil
}

// IsolatedEnviron keeps only locale/timezone settings from base, then installs
// the run paths. Credentials, proxy configuration, provider tokens, and other
// ambient authority are dropped rather than blocklisted.
func (r *RunRoot) IsolatedEnviron(base []string) ([]string, error) {
	overlay, err := r.Environment()
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(overlay)+4)
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !safeInheritedEnvironmentKey(key) {
			continue
		}
		values[key] = value
	}
	for key, value := range overlay {
		values[key] = value
	}
	return sortedEnvironment(values), nil
}

func safeInheritedEnvironmentKey(key string) bool {
	return key == "LANG" || key == "LANGUAGE" || key == "TZ" || key == "TERM" ||
		key == "COLORTERM" || key == "NO_COLOR" || key == "FORCE_COLOR" ||
		strings.HasPrefix(key, "LC_")
}

func sortedEnvironment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

// Cleanup removes the entire run tree. It is idempotent. After Cleanup, all
// other RunRoot methods fail with ErrRunRootClosed.
func (r *RunRoot) Cleanup() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	if r.paths.Root == "" {
		return errors.New("briefcase: refusing to clean empty run root")
	}
	if err := os.RemoveAll(r.paths.Root); err != nil {
		return fmt.Errorf("briefcase: clean run root: %w", err)
	}
	r.closed = true
	return nil
}

// Close implements io.Closer-style cleanup.
func (r *RunRoot) Close() error { return r.Cleanup() }

func (r *RunRoot) isClosed() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

// ClaimHarness makes one RunRoot single-use at the harness boundary. Directory
// emptiness alone is insufficient: a caller could delete workspace files while
// leaving transcripts, caches, or device-derived state behind.
func (r *RunRoot) ClaimHarness() error {
	if r == nil {
		return ErrRunRootClosed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRunRootClosed
	}
	if r.claimed {
		return ErrRunRootClaimed
	}
	r.claimed = true
	return nil
}
