package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ContextFile represents a workspace context file embedded in the system prompt.
type ContextFile struct {
	Path    string // relative path (e.g., "AGENTS.md")
	Content string
}

// contextFileNames lists workspace context files in load order.
// Matches src/agents/workspace/workspace.ts DEFAULT_*_FILENAME constants.
var contextFileNames = []string{
	"AGENTS.md",
	"SOUL.md",
	"TOOLS.md",
	"IDENTITY.md",
	"USER.md",
}

const (
	// maxContextFileChars is the maximum characters per context file.
	maxContextFileChars = 20_000
	// maxContextTotalChars is the maximum total characters for all context files.
	maxContextTotalChars = 150_000
	// ctxCacheRevalidateInterval forces a full re-scan after this duration
	// to detect newly added or deleted context files.
	// Extended for single-user DGX Spark: config files rarely change mid-session.
	ctxCacheRevalidateInterval = 5 * time.Minute
)

// ResetContextFileCacheForTest clears all prompt caches.
// Intended for tests to avoid cross-test state leakage.
func ResetContextFileCacheForTest() {
	Cache.Reset()
}

// LoadContextFiles scans the workspace directory and its ancestors for known
// context files (AGENTS.md, SOUL.md, TOOLS.md, etc.) and returns
// their contents. Files closer to the workspace root take precedence.
// This mirrors the Node.js behavior of walking up the directory tree.
//
// Results are cached using mtime-based validation: on subsequent calls for the
// same workspace, only os.Stat is performed (skipping EvalSymlinks + ReadFile)
// unless a file has been modified or the revalidation interval has elapsed.
func LoadContextFiles(workspaceDir string, opts ...LoadContextOption) []ContextFile {
	if workspaceDir == "" {
		return nil
	}

	var cfg loadContextConfig
	for _, o := range opts {
		o(&cfg)
	}

	// Frozen snapshot: return cached files if this session already loaded.
	if cfg.sessionKey != "" {
		if frozen, ok := Cache.SessionSnapshot(cfg.sessionKey); ok {
			return frozen
		}
	}

	Cache.LockCtx()
	defer Cache.UnlockCtx()

	var files []ContextFile
	if cached, ok := Cache.ContextFiles(workspaceDir); ok {
		files = cached
	} else {
		var resolved map[string]time.Time
		files, resolved = loadContextFilesFromDisk(workspaceDir)
		Cache.SetContextFiles(workspaceDir, files, resolved)
	}

	// Freeze for this session.
	if cfg.sessionKey != "" {
		Cache.SetSessionSnapshot(cfg.sessionKey, files)
	}

	return files
}

// loadContextConfig holds options for LoadContextFiles.
type loadContextConfig struct {
	sessionKey string // non-empty → use/populate frozen session snapshot
}

// LoadContextOption configures LoadContextFiles behavior.
type LoadContextOption func(*loadContextConfig)

// WithSessionSnapshot enables the frozen snapshot pattern: on first call
// for a given session key the loaded context files are cached and returned
// unchanged for all subsequent calls within that session. This keeps the
// system prompt stable across turns so the LLM prefix cache is not
// invalidated by mid-session context file writes.
func WithSessionSnapshot(sessionKey string) LoadContextOption {
	return func(c *loadContextConfig) { c.sessionKey = sessionKey }
}

// ClearSessionSnapshot removes the frozen context files for a session.
// Call on session reset or terminal state transition.
func ClearSessionSnapshot(sessionKey string) {
	Cache.ClearSession(sessionKey)
}

// loadContextFilesFromDisk performs the actual filesystem scan.
func loadContextFilesFromDisk(workspaceDir string) ([]ContextFile, map[string]time.Time) { //nolint:gocritic // unnamedResult — naming would shadow local vars
	searchDirs := collectSearchDirs(workspaceDir)

	var files []ContextFile
	totalChars := 0
	seen := make(map[string]struct{})            // track resolved paths for dedup
	resolvedMtimes := make(map[string]time.Time) // for cache validation

	for _, name := range contextFileNames {
		for _, dir := range searchDirs {
			path := filepath.Join(dir, name)

			// Follow symlinks in case context files are symlinked.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				continue
			}

			// Skip if we already loaded this resolved path.
			if _, ok := seen[resolved]; ok {
				break
			}

			info, err := os.Stat(resolved)
			if err != nil {
				continue
			}

			data, err := os.ReadFile(resolved)
			if err != nil {
				continue
			}

			content := string(data)
			if content == "" {
				continue
			}

			// Record mtime for cache validation.
			resolvedMtimes[resolved] = info.ModTime()

			// Skip if this would exceed total budget.
			if totalChars+len(content) > maxContextTotalChars {
				remaining := maxContextTotalChars - totalChars
				if remaining <= 0 {
					break
				}
				content = truncateContent(content, remaining)
			}

			// Truncate individual file if too large.
			if len(content) > maxContextFileChars {
				content = truncateContent(content, maxContextFileChars)
			}

			// Content-based dedup (handles symlinks pointing to same file).
			isDup := false
			for _, existing := range files {
				if existing.Content == content {
					isDup = true
					break
				}
			}
			if isDup {
				seen[resolved] = struct{}{}
				break
			}

			// Use relative label: if from workspace root, just the filename;
			// otherwise include relative path hint.
			label := name
			if dir != workspaceDir {
				rel, _ := filepath.Rel(workspaceDir, filepath.Join(dir, name))
				if rel != "" {
					label = rel
				}
			}

			files = append(files, ContextFile{
				Path:    label,
				Content: content,
			})
			totalChars += len(content)
			seen[resolved] = struct{}{}
			break // Found for this filename, don't search further up
		}
	}

	return files, resolvedMtimes
}

// collectSearchDirs returns the workspace dir plus its ancestors, stopping
// at the user's home directory or filesystem root (max 10 levels).
func collectSearchDirs(workspaceDir string) []string {
	dirs := []string{workspaceDir}

	home, _ := os.UserHomeDir()
	current := workspaceDir
	for range 10 {
		parent := filepath.Dir(current)
		if parent == current {
			break // reached filesystem root
		}
		if home != "" && parent == home {
			// Include home but don't go above it.
			dirs = append(dirs, parent)
			break
		}
		dirs = append(dirs, parent)
		current = parent
	}

	return dirs
}

// truncateContent truncates content to maxChars using head (70%) + marker + tail (20%).
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	headSize := maxChars * 70 / 100
	tailSize := maxChars * 20 / 100
	marker := "\n\n[...truncated...]\n\n"

	head := content[:headSize]
	tail := content[len(content)-tailSize:]
	return head + marker + tail
}

// FormatContextFilesForPrompt formats loaded context files for inclusion
// in the system prompt. If SOUL.md is present, an explicit instruction to
// embody its persona/tone is injected (mirrors the TS system-prompt behavior).
func FormatContextFilesForPrompt(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Project Context\n\n")

	// Detect SOUL.md presence.
	hasSoulFile := false
	for _, f := range files {
		base := filepath.Base(f.Path)
		if strings.EqualFold(base, "SOUL.md") {
			hasSoulFile = true
			break
		}
	}

	sb.WriteString("The following project context files have been loaded:\n")
	if hasSoulFile {
		sb.WriteString("If SOUL.md is present, embody its persona and tone. Avoid stiff, generic replies; follow its guidance unless higher-priority instructions override it.\n")
	}
	sb.WriteString("\n")

	for _, f := range files {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", f.Path, strings.TrimSpace(f.Content))
	}

	return sb.String()
}
