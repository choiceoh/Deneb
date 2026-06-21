package prompt

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
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
	"MEMORY.md",
}

const (
	// maxContextFileChars is the maximum bytes per context file.
	// Sized for single-user DGX Spark: typical AGENTS.md/SOUL.md files
	// fit comfortably under 8K chars (~2K tokens). Oversized files are
	// head+tail truncated so core rules are preserved.
	maxContextFileChars = 8_000
	// maxMemoryFileChars is the per-file budget for MEMORY.md. The agent's
	// auto-recorded long-term memory routinely outgrows rule files (observed
	// 176KB in production); the default 8K cap silently hid ~95% of it every
	// turn. 32K keeps the curated head sections plus the recent tail entries.
	// The dynamic system block is byte-stable per session, so the trailing
	// message cache markers still amortize the extra tokens after turn one.
	maxMemoryFileChars = 32_000
	// maxContextTotalChars is the maximum total bytes for all context files.
	// 5 rule files x 8K + MEMORY.md 32K = 72K bytes worst case, leaving ample
	// budget for conversation history and tool schemas.
	maxContextTotalChars = 72_000
	// ctxCacheRevalidateInterval forces a full re-scan after this duration
	// to detect newly added or deleted context files.
	// Extended for single-user DGX Spark: config files rarely change mid-session.
	ctxCacheRevalidateInterval = 5 * time.Minute
)

// contextFileCharBudget returns the per-file byte budget for a context file.
func contextFileCharBudget(name string) int {
	if name == "MEMORY.md" {
		return maxMemoryFileChars
	}
	return maxContextFileChars
}

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

	// Append session-specific extra context (e.g. the operator's org chart),
	// rendered once here on the build path. The frozen-snapshot short-circuit
	// above returns before reaching this on every later turn, so each source runs
	// once per session — not per turn. Copy first so the shared workspaceDir
	// cache is never mutated with session-specific entries.
	if len(cfg.extraSources) > 0 {
		combined := append([]ContextFile(nil), files...)
		for _, src := range cfg.extraSources {
			combined = append(combined, src()...)
		}
		files = combined
	}

	// Freeze for this session.
	if cfg.sessionKey != "" {
		Cache.SetSessionSnapshot(cfg.sessionKey, files)
	}

	return files
}

// loadContextConfig holds options for LoadContextFiles.
type loadContextConfig struct {
	sessionKey   string                 // non-empty → use/populate frozen session snapshot
	extraSources []func() []ContextFile // lazy session-specific context, run once per session
}

// LoadContextOption configures LoadContextFiles behavior.
type LoadContextOption func(*loadContextConfig)

// WithExtraSource adds a lazily-rendered context source folded into the frozen
// session snapshot. The function runs once per session (on the snapshot-build
// path), so per-turn calls reuse the frozen result instead of re-rendering.
// Used for generated context (the org chart) that is not a file on disk.
func WithExtraSource(fn func() []ContextFile) LoadContextOption {
	return func(c *loadContextConfig) {
		if fn != nil {
			c.extraSources = append(c.extraSources, fn)
		}
	}
}

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

			// Effective budget: per-file cap, clipped by the remaining total.
			budget := contextFileCharBudget(name)
			if remaining := maxContextTotalChars - totalChars; remaining < budget {
				if remaining <= 0 {
					break
				}
				budget = remaining
			}
			if len(content) > budget {
				// Truncation drops real memory content — surface it so the
				// operator learns the file needs trimming (not just the LLM).
				slog.Warn("context file exceeds budget; head/tail truncated",
					"file", name, "sizeBytes", len(content), "budgetBytes", budget)
				content = truncateContent(content, budget)
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
// at the user's home directory or filesystem root (max 6 levels).
// Limit tightened for single-user DGX Spark deployment where deep ancestor
// walks yield no additional context files but inflate the prompt budget.
func collectSearchDirs(workspaceDir string) []string {
	dirs := []string{workspaceDir}

	home, _ := os.UserHomeDir()
	current := workspaceDir
	for range 6 {
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
// Cuts are aligned to UTF-8 rune boundaries so multi-byte characters (Korean,
// emoji, CJK) are never split mid-rune — the caps are stated in bytes, but the
// slicing must not emit invalid UTF-8 into the system prompt.
// The marker states how much was dropped so the model (and anyone reading the
// assembled prompt) knows the file is incomplete instead of silently partial.
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	headSize := maxChars * 70 / 100
	tailSize := maxChars * 20 / 100
	omittedKB := (len(content) - headSize - tailSize + 1023) / 1024
	marker := fmt.Sprintf("\n\n[...truncated: ~%dKB omitted — file exceeds its context budget; trim it or move durable notes into the wiki...]\n\n", omittedKB)

	head := clipHeadUTF8(content, headSize)
	tail := clipTailUTF8(content, tailSize)
	return head + marker + tail
}

// clipHeadUTF8 returns a prefix of s no longer than n bytes, ending on a rune
// boundary. If n lands inside a multi-byte rune, the returned prefix is
// shortened rather than extended so the total never exceeds n.
func clipHeadUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// clipTailUTF8 returns a suffix of s no longer than n bytes, starting on a
// rune boundary. If n lands inside a multi-byte rune the suffix is shortened
// (start moves forward) so the total stays within n.
func clipTailUTF8(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
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
