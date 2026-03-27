package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// manualDocCacheTTL is the TTL for the doc tree index cache.
// Docs rarely change in a running gateway, so 60s is generous.
const manualDocCacheTTL = 60 * time.Second

// manualMaxReadChars caps the output of the read action to avoid context bloat.
const manualMaxReadChars = 8000

// manualMaxSearchResults caps keyword search results.
const manualMaxSearchResults = 15

// --- Doc tree index cache ---

type docEntry struct {
	Path    string // relative to docs/, e.g. "concepts/session"
	Title   string // from frontmatter
	Summary string // from frontmatter
}

type docTreeCache struct {
	mu        sync.Mutex
	docsDir   string
	entries   []docEntry
	expiresAt time.Time
}

var manualTreeCache = &docTreeCache{}

func (c *docTreeCache) get(docsDir string) ([]docEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.docsDir != docsDir {
		return nil, false
	}
	if time.Now().After(c.expiresAt) {
		return nil, false
	}
	return c.entries, true
}

func (c *docTreeCache) set(docsDir string, entries []docEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docsDir = docsDir
	c.entries = entries
	c.expiresAt = time.Now().Add(manualDocCacheTTL)
}

// --- Doc content cache (mtime-based, same pattern as tool_memory.go) ---

type manualContentEntry struct {
	content string
	mtime   time.Time
}

var manualContentCacheMu sync.Mutex
var manualContentCacheMap = make(map[string]*manualContentEntry)

func readDocFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	mtime := info.ModTime()

	manualContentCacheMu.Lock()
	if entry, ok := manualContentCacheMap[path]; ok && entry.mtime.Equal(mtime) {
		content := entry.content
		manualContentCacheMu.Unlock()
		return content, nil
	}
	manualContentCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	manualContentCacheMu.Lock()
	manualContentCacheMap[path] = &manualContentEntry{content: content, mtime: mtime}
	manualContentCacheMu.Unlock()

	return content, nil
}

// --- Frontmatter parsing ---

// parseFrontmatter extracts title and summary from YAML frontmatter.
// Returns (title, summary, bodyWithoutFrontmatter).
func parseFrontmatter(content string) (string, string, string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", content
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return "", "", content
	}
	fm := content[4 : 4+end]
	body := content[4+end+4:] // skip past closing "---\n"

	var title, summary string
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			title = strings.Trim(strings.TrimPrefix(line, "title:"), " \"'")
		} else if strings.HasPrefix(line, "summary:") {
			summary = strings.Trim(strings.TrimPrefix(line, "summary:"), " \"'")
		}
	}
	return title, summary, body
}

// --- Doc tree scanning ---

func scanDocTree(docsDir string) []docEntry {
	var entries []docEntry

	_ = filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(docsDir, path)
		if rel == "" {
			return nil
		}
		// Skip generated and asset files.
		if strings.HasPrefix(rel, ".generated/") || strings.HasPrefix(rel, "assets/") {
			return nil
		}

		content, readErr := readDocFile(path)
		if readErr != nil {
			return nil
		}

		title, summary, _ := parseFrontmatter(content)
		// Strip .md extension for topic path.
		topicPath := strings.TrimSuffix(rel, ".md")

		entries = append(entries, docEntry{
			Path:    topicPath,
			Title:   title,
			Summary: summary,
		})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

func getDocTree(docsDir string) []docEntry {
	if cached, ok := manualTreeCache.get(docsDir); ok {
		return cached
	}
	entries := scanDocTree(docsDir)
	manualTreeCache.set(docsDir, entries)
	return entries
}

// --- Schema ---

func systemManualToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"topics", "search", "read", "guides"},
				"description": "topics: browse doc tree, search: keyword search, read: read a doc, guides: AI-curated internal system guides",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Keyword(s) for search action",
			},
			"topic": map[string]any{
				"type":        "string",
				"description": "For topics: optional category filter (e.g. 'gateway'). For read: doc path (e.g. 'concepts/session'). For guides: guide name (e.g. 'aurora', 'vega')",
			},
		},
		"required": []string{"action"},
	}
}

// --- Tool implementation ---

func toolSystemManual(workspaceDir string) ToolFunc {
	docsDir := filepath.Join(workspaceDir, "docs")

	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Query  string `json:"query"`
			Topic  string `json:"topic"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid polaris params: %w", err)
		}

		switch p.Action {
		case "topics":
			return manualTopics(docsDir, p.Topic)
		case "search":
			return manualSearch(docsDir, p.Query)
		case "read":
			return manualRead(docsDir, p.Topic)
		case "guides":
			return manualGuides(p.Topic)
		default:
			return "", fmt.Errorf("unknown action %q (valid: topics, search, read, guides)", p.Action)
		}
	}
}

// --- topics action ---

func manualTopics(docsDir, filter string) (string, error) {
	if _, err := os.Stat(docsDir); err != nil {
		return "No docs/ directory found in workspace.", nil
	}

	entries := getDocTree(docsDir)
	if len(entries) == 0 {
		return "No documentation files found in docs/.", nil
	}

	// Group by top-level directory.
	type categoryGroup struct {
		name    string
		entries []docEntry
	}
	groups := make(map[string]*categoryGroup)
	var groupOrder []string

	for _, e := range entries {
		parts := strings.SplitN(e.Path, "/", 2)
		cat := parts[0]
		if len(parts) == 1 {
			cat = "." // root-level docs
		}

		// Apply category filter if specified.
		if filter != "" && cat != filter {
			continue
		}

		if _, ok := groups[cat]; !ok {
			groups[cat] = &categoryGroup{name: cat}
			groupOrder = append(groupOrder, cat)
		}
		groups[cat].entries = append(groups[cat].entries, e)
	}

	sort.Strings(groupOrder)

	var sb strings.Builder
	total := len(entries)
	if filter != "" {
		total = 0
		for _, g := range groups {
			total += len(g.entries)
		}
		fmt.Fprintf(&sb, "Deneb System Manual — %s/ (%d docs)\n\n", filter, total)
	} else {
		fmt.Fprintf(&sb, "Deneb System Manual (%d docs)\n\n", total)
	}

	for _, cat := range groupOrder {
		g := groups[cat]
		if cat == "." {
			fmt.Fprintf(&sb, "(root) (%d docs)\n", len(g.entries))
		} else {
			fmt.Fprintf(&sb, "%s/ (%d docs)\n", cat, len(g.entries))
		}
		for i, e := range g.entries {
			prefix := "  |-- "
			if i == len(g.entries)-1 {
				prefix = "  `-- "
			}
			label := e.Title
			if label == "" {
				// Fall back to filename.
				parts := strings.SplitN(e.Path, "/", 2)
				if len(parts) > 1 {
					label = parts[1]
				} else {
					label = parts[0]
				}
			}
			if e.Summary != "" {
				fmt.Fprintf(&sb, "%s%s — %s\n", prefix, e.Path, label)
			} else {
				fmt.Fprintf(&sb, "%s%s — %s\n", prefix, e.Path, label)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Use polaris(action:'read', topic:'<path>') to read a doc.\n")
	sb.WriteString("Use polaris(action:'search', query:'<keyword>') to search.\n")
	return sb.String(), nil
}

// --- search action ---

func manualSearch(docsDir, query string) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for search action")
	}
	if _, err := os.Stat(docsDir); err != nil {
		return "No docs/ directory found in workspace.", nil
	}

	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return "No keywords provided.", nil
	}

	entries := getDocTree(docsDir)

	type searchMatch struct {
		File    string
		Line    int
		Snippet string
	}

	var matches []searchMatch
	for _, e := range entries {
		absPath := filepath.Join(docsDir, e.Path+".md")
		content, err := readDocFile(absPath)
		if err != nil {
			continue
		}
		_, _, body := parseFrontmatter(content)
		lines := strings.Split(body, "\n")

		matchedLines := make(map[int]bool)
		for i, line := range lines {
			if matchedLines[i] {
				continue
			}
			lower := strings.ToLower(line)
			for _, kw := range keywords {
				if strings.Contains(lower, kw) {
					start := i - 2
					if start < 0 {
						start = 0
					}
					end := i + 3
					if end > len(lines) {
						end = len(lines)
					}
					for j := start; j < end; j++ {
						matchedLines[j] = true
					}
					snippet := strings.Join(lines[start:end], "\n")
					matches = append(matches, searchMatch{
						File:    e.Path,
						Line:    i + 1,
						Snippet: snippet,
					})
					break
				}
			}
			if len(matches) >= manualMaxSearchResults {
				break
			}
		}
		if len(matches) >= manualMaxSearchResults {
			break
		}
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for %q in documentation.", query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d matches for %q:\n\n", len(matches), query)
	for _, m := range matches {
		fmt.Fprintf(&sb, "### %s (line %d)\n%s\n\n", m.File, m.Line, m.Snippet)
	}
	if len(matches) >= manualMaxSearchResults {
		sb.WriteString("... (showing first 15 results, refine your query for more specific results)\n")
	}
	return sb.String(), nil
}

// --- read action ---

func manualRead(docsDir, topic string) (string, error) {
	if topic == "" {
		return "", fmt.Errorf("topic is required for read action (e.g. 'concepts/session')")
	}

	absPath := filepath.Join(docsDir, topic+".md")
	content, err := readDocFile(absPath)
	if err != nil {
		// Try with /index.md for directory topics.
		absPath = filepath.Join(docsDir, topic, "index.md")
		content, err = readDocFile(absPath)
		if err != nil {
			return fmt.Sprintf("Document not found: %q. Use polaris(action:'topics') to browse available docs.", topic), nil
		}
	}

	_, _, body := parseFrontmatter(content)
	body = strings.TrimSpace(body)

	// Truncate if too long.
	if len(body) > manualMaxReadChars {
		headSize := manualMaxReadChars * 70 / 100
		tailSize := manualMaxReadChars * 20 / 100
		body = body[:headSize] + "\n\n... [truncated — use search for specific sections] ...\n\n" + body[len(body)-tailSize:]
	}

	return body, nil
}

// --- guides action ---

// guideEntry represents a curated guide.
type guideEntry struct {
	Key     string
	Title   string
	Summary string
	Content string
}

// builtinGuideOrder defines the display order for guides.
var builtinGuideOrder = []string{
	"aurora", "vega", "agent-loop", "compaction", "tools",
	"system-prompt", "memory", "sessions", "architecture", "channels",
}

// builtinGuides contains AI-curated system knowledge.
// Each guide is ~2-4K chars of dense, actionable information.
var builtinGuides = map[string]guideEntry{
	"aurora": {
		Key:     "aurora",
		Title:   "Aurora Context Engine",
		Summary: "Context assembly lifecycle, token budgeting, aurora tools",
		Content: auroraGuide,
	},
	"vega": {
		Key:     "vega",
		Title:   "Vega Search Engine",
		Summary: "BM25 + semantic hybrid search, FTS5, embedding backends",
		Content: vegaGuide,
	},
	"agent-loop": {
		Key:     "agent-loop",
		Title:   "Agent Execution Loop",
		Summary: "LLM->tool loop, event streams, hooks, timeouts",
		Content: agentLoopGuide,
	},
	"compaction": {
		Key:     "compaction",
		Title:   "Message Compaction",
		Summary: "Hierarchical summarization, fresh-tail protection, memory flush",
		Content: compactionGuide,
	},
	"tools": {
		Key:     "tools",
		Title:   "Tool System Deep Dive",
		Summary: "ToolDef/ToolRegistry, parallel execution, $ref chaining, post-processing",
		Content: toolsGuide,
	},
	"system-prompt": {
		Key:     "system-prompt",
		Title:   "System Prompt Assembly",
		Summary: "Fixed sections, bootstrap injection, prompt modes, cache breakpoints",
		Content: systemPromptGuide,
	},
	"memory": {
		Key:     "memory",
		Title:   "Memory System",
		Summary: "Daily + long-term memory, search/get, semantic recall, auto-flush",
		Content: memoryGuide,
	},
	"sessions": {
		Key:     "sessions",
		Title:   "Session Lifecycle",
		Summary: "State machine, session kinds, spawn/steer/kill",
		Content: sessionsGuide,
	},
	"architecture": {
		Key:     "architecture",
		Title:   "System Architecture",
		Summary: "Go + Rust FFI + Node.js, IPC boundaries, hardware profiles",
		Content: architectureGuide,
	},
	"channels": {
		Key:     "channels",
		Title:   "Channel System",
		Summary: "Plugin registry, Telegram optimization, routing, groups",
		Content: channelsGuide,
	},
}

func manualGuides(topic string) (string, error) {
	if topic == "" {
		// List all guides.
		var sb strings.Builder
		sb.WriteString("Deneb System Guides (AI-curated)\n\n")
		for _, key := range builtinGuideOrder {
			g := builtinGuides[key]
			fmt.Fprintf(&sb, "  %-16s — %s\n", g.Key, g.Summary)
		}
		sb.WriteString("\nUse polaris(action:'guides', topic:'<key>') to read a guide.\n")
		return sb.String(), nil
	}

	g, ok := builtinGuides[topic]
	if !ok {
		return fmt.Sprintf("Unknown guide %q. Use polaris(action:'guides') to list available guides.", topic), nil
	}
	return fmt.Sprintf("# %s\n\n%s", g.Title, g.Content), nil
}
