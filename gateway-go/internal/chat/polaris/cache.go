// Package polaris implements the polaris agent tool: a searchable index of
// Deneb's documentation tree plus AI-curated system guides, with an AI-powered
// ask action that autonomously gathers relevant knowledge and synthesizes answers.
package polaris

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

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// LLMSynthesizer calls a local LLM for answer synthesis.
// Injected from the chat package to avoid circular imports.
type LLMSynthesizer func(ctx context.Context, system, userMessage string, maxTokens int) (string, error)

// HealthChecker checks if the local LLM is available.
type HealthChecker func() bool

// ToolExecutor executes a named tool with JSON input and returns the result.
// Matches agent.ToolExecutor; injected to give Polaris read access to the entire codebase.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// readOnlyAllowed is the set of tools Polaris is permitted to call.
// Write, edit, exec, and any mutation tools are deliberately excluded.
var readOnlyAllowed = map[string]bool{
	"read":    true,
	"grep":    true,
	"find":    true,
	"tree":    true,
	"analyze": true,
	"diff":    true,
}

// ReadOnlyExecutor wraps a ToolExecutor and restricts it to read-only tools.
type ReadOnlyExecutor struct {
	Inner ToolExecutor
}

// Execute delegates to the inner executor only for allowed read-only tools.
func (r *ReadOnlyExecutor) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if !readOnlyAllowed[name] {
		return "", fmt.Errorf("polaris: tool %q is not allowed (read-only access)", name)
	}
	return r.Inner.Execute(ctx, name, input)
}

// polarisCacheTTL is the TTL for the doc tree index cache.
// Docs rarely change in a running gateway, so 60s is generous.
const polarisCacheTTL = 60 * time.Second

// polarisMaxReadChars caps the output of the read action to avoid context bloat.
const polarisMaxReadChars = 8000

// polarisMaxSearchResults caps keyword search results.
const polarisMaxSearchResults = 15

// ask action constants.
const (
	polarisAskMaxContextChars = 15000 // max chars of gathered content for LLM input
	polarisAskMaxTokens       = 4096  // LLM output token budget
	polarisAskMaxOutputChars  = 3500  // hard cap on final output (Telegram safety)
	polarisAskMaxDocs         = 3     // max full docs to read
	polarisAskMaxGuides       = 2     // max full guides to include
	polarisAskMaxCodeFiles    = 3     // max source code files to read
	polarisAskPerSourceChars  = 4000  // max chars per individual source
	polarisAskGrepTimeout     = 10 * time.Second
)

const polarisAskSystemPrompt = `You are Polaris, Deneb's system knowledge agent.
Given a question, relevant documentation, system guides, and source code, provide a direct, comprehensive answer.
Rules:
- Answer in the same language as the question (Korean if Korean, English if English).
- Be precise. Cite file paths and line numbers when referencing code.
- When referencing docs/guides, mention the guide or doc name.
- Output for Telegram (max 3500 chars). Be concise but complete.
- Use markdown formatting sparingly (bold for key terms, code blocks for code).
- If the available context doesn't cover the topic, say so clearly.
- No preamble or pleasantries. Direct answer only.`

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

var polarisTreeCache = &docTreeCache{}

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
	c.expiresAt = time.Now().Add(polarisCacheTTL)
}

// --- Doc content cache (mtime-based, same pattern as tool_memory.go) ---

type polarisContentEntry struct {
	content string
	mtime   time.Time
}

var polarisContentCacheMu sync.Mutex
var polarisContentCacheMap = make(map[string]*polarisContentEntry)

func readDocFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	mtime := info.ModTime()

	polarisContentCacheMu.Lock()
	if entry, ok := polarisContentCacheMap[path]; ok && entry.mtime.Equal(mtime) {
		content := entry.content
		polarisContentCacheMu.Unlock()
		return content, nil
	}
	polarisContentCacheMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	polarisContentCacheMu.Lock()
	polarisContentCacheMap[path] = &polarisContentEntry{content: content, mtime: mtime}
	polarisContentCacheMu.Unlock()

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
	if cached, ok := polarisTreeCache.get(docsDir); ok {
		return cached
	}
	entries := scanDocTree(docsDir)
	polarisTreeCache.set(docsDir, entries)
	return entries
}

// --- Docs directory resolution ---

// resolveDocsDir finds the docs/ directory by checking multiple locations:
//  1. workspaceDir/docs (agent workspace)
//  2. Executable's parent directories (binary lives in repo under gateway-go/)
//  3. Current working directory ancestors (gateway often runs from repo root)
//
// Result is cached after first successful resolution.
var (
	resolvedDocsDir     string
	resolvedDocsDirOnce sync.Once
)

func resolveDocsDir(workspaceDir string) string {
	resolvedDocsDirOnce.Do(func() {
		// 1. Check workspace directory.
		candidate := filepath.Join(workspaceDir, "docs")
		if hasDocsContent(candidate) {
			resolvedDocsDir = candidate
			return
		}

		// 2. Walk up from executable path (e.g. /repo/gateway-go/deneb-gateway → /repo/docs).
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			for i := 0; i < 5; i++ {
				candidate = filepath.Join(dir, "docs")
				if hasDocsContent(candidate) {
					resolvedDocsDir = candidate
					return
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}

		// 3. Walk up from cwd (gateway often started from repo root).
		if cwd, err := os.Getwd(); err == nil {
			dir := cwd
			for i := 0; i < 5; i++ {
				candidate = filepath.Join(dir, "docs")
				if hasDocsContent(candidate) {
					resolvedDocsDir = candidate
					return
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}

		// Fallback: use workspace/docs even if empty (preserves old behavior).
		resolvedDocsDir = filepath.Join(workspaceDir, "docs")
	})
	return resolvedDocsDir
}

// hasDocsContent checks that a directory exists and contains at least one .md file.
func hasDocsContent(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	// Quick check: look for any .md file in the top two levels.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
		if e.IsDir() {
			subPath := filepath.Join(dir, e.Name())
			subEntries, err := os.ReadDir(subPath)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() && strings.HasSuffix(se.Name(), ".md") {
					return true
				}
			}
		}
	}
	return false
}

// --- Tool implementation ---

// Deps holds injected dependencies for the polaris handler.
type Deps struct {
	LLM     LLMSynthesizer // local LLM for answer synthesis (nil = fallback mode)
	Health  HealthChecker   // LLM health check (nil = fallback mode)
	Tools   ToolExecutor    // tool executor for codebase-wide read access (nil = docs-only)
}

// NewHandler returns the polaris tool handler function for use with ToolRegistry.
// This handler supports all actions; ask falls back to raw results without LLM.
func NewHandler(workspaceDir string) func(context.Context, json.RawMessage) (string, error) {
	return newHandler(workspaceDir, Deps{})
}

// NewHandlerWithDeps returns the polaris tool handler with full AI agent capabilities.
// Injected deps give Polaris LLM synthesis and unrestricted codebase read access.
func NewHandlerWithDeps(workspaceDir string, deps Deps) func(context.Context, json.RawMessage) (string, error) {
	return newHandler(workspaceDir, deps)
}

func newHandler(workspaceDir string, deps Deps) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		docsDir := resolveDocsDir(workspaceDir)
		var p struct {
			Question string `json:"question"`
		}
		if err := jsonutil.UnmarshalInto("polaris params", input, &p); err != nil {
			return "", err
		}

		return polarisAsk(ctx, docsDir, workspaceDir, p.Question, deps)
	}
}

// --- ask action ---

// polarisAsk autonomously gathers relevant docs, guides, and source code, then synthesizes an answer.
func polarisAsk(ctx context.Context, docsDir, workspaceDir, question string, deps Deps) (string, error) {
	if question == "" {
		return "", fmt.Errorf("question is required for ask action")
	}

	// Phase 1: Gather relevant knowledge from all sources.
	keywords := extractSearchKeywords(question)
	kwList := strings.Fields(strings.ToLower(keywords))

	// 1a. Search docs and builtin guides.
	searchResults := polarisSearchInternal(docsDir, keywords)

	// 1b. Score guides by relevance independently.
	topGuides := scoreAndSelectGuides(kwList, polarisAskMaxGuides)

	// 1c. Read top doc matches in full.
	var docContents []namedContent
	seen := make(map[string]bool)
	for _, m := range searchResults {
		if m.IsGuide {
			continue
		}
		if seen[m.Path] {
			continue
		}
		seen[m.Path] = true
		body, err := polarisRead(docsDir, m.Path)
		if err != nil || strings.HasPrefix(body, "Document not found") {
			continue
		}
		docContents = append(docContents, namedContent{
			name:    "docs/" + m.Path,
			content: truncateChars(body, polarisAskPerSourceChars),
		})
		if len(docContents) >= polarisAskMaxDocs {
			break
		}
	}

	// 1d. Collect guide content.
	var guideContents []namedContent
	for _, g := range topGuides {
		guideContents = append(guideContents, namedContent{
			name:    g.Key + " (guide)",
			content: truncateChars(g.Content, polarisAskPerSourceChars),
		})
	}

	// 1e. Search source code via ToolExecutor (grep + read).
	var codeContents []namedContent
	if deps.Tools != nil {
		codeContents = gatherCodeContext(ctx, deps.Tools, workspaceDir, keywords)
	}

	// Phase 2: Assemble context for LLM.
	contextText := assembleAskContext(question, docContents, guideContents, codeContents)

	// Phase 3: Check LLM availability and synthesize.
	if deps.LLM == nil || deps.Health == nil || !deps.Health() {
		return buildAskFallback(question, docContents, guideContents, codeContents), nil
	}

	result, err := deps.LLM(ctx, polarisAskSystemPrompt, contextText, polarisAskMaxTokens)
	if err != nil {
		return buildAskFallback(question, docContents, guideContents, codeContents), nil
	}

	// Enforce output length for Telegram.
	result = strings.TrimSpace(result)
	if len(result) > polarisAskMaxOutputChars {
		result = result[:polarisAskMaxOutputChars] + "\n..."
	}

	return result, nil
}

type namedContent struct {
	name    string
	content string
}

// gatherCodeContext uses the injected ToolExecutor to grep the codebase
// and read the most relevant source files.
func gatherCodeContext(ctx context.Context, tools ToolExecutor, workspaceDir, keywords string) []namedContent {
	grepCtx, cancel := context.WithTimeout(ctx, polarisAskGrepTimeout)
	defer cancel()

	// Grep across source code for the most specific keyword (longest = most specific).
	kwList := strings.Fields(keywords)
	if len(kwList) == 0 {
		return nil
	}
	sort.Slice(kwList, func(i, j int) bool {
		return len(kwList[i]) > len(kwList[j])
	})

	// Use the top 2 most specific keywords for grep.
	grepKeywords := kwList
	if len(grepKeywords) > 2 {
		grepKeywords = grepKeywords[:2]
	}

	// Grep source files (Go + Rust + Proto), deduplicate file paths.
	fileHits := make(map[string]int) // path → hit count
	for _, kw := range grepKeywords {
		grepInput, _ := json.Marshal(map[string]any{
			"pattern": kw,
			"path":    workspaceDir,
			"include": "*.go,*.rs,*.proto",
		})
		result, err := tools.Execute(grepCtx, "grep", grepInput)
		if err != nil {
			continue
		}
		// Parse grep output: each line is "path:line:content".
		for _, line := range strings.Split(result, "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, ":", 3)
			if len(parts) < 2 {
				continue
			}
			path := parts[0]
			// Skip test files and generated files for relevance.
			if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "_gen.") || strings.Contains(path, "/gen/") {
				continue
			}
			fileHits[path]++
		}
	}

	if len(fileHits) == 0 {
		return nil
	}

	// Rank files by hit count and select top N.
	type fileScore struct {
		path  string
		score int
	}
	var ranked []fileScore
	for p, s := range fileHits {
		ranked = append(ranked, fileScore{p, s})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > polarisAskMaxCodeFiles {
		ranked = ranked[:polarisAskMaxCodeFiles]
	}

	// Read each file via the read tool.
	var results []namedContent
	for _, f := range ranked {
		readInput, _ := json.Marshal(map[string]any{
			"file_path": f.path,
			"limit":     150, // first 150 lines — enough for structure
		})
		content, err := tools.Execute(ctx, "read", readInput)
		if err != nil {
			continue
		}
		// Make path relative to workspace for readability.
		relPath := f.path
		if rel, err := filepath.Rel(workspaceDir, f.path); err == nil {
			relPath = rel
		}
		results = append(results, namedContent{
			name:    relPath,
			content: truncateChars(content, polarisAskPerSourceChars),
		})
	}
	return results
}

// extractSearchKeywords extracts meaningful search terms from a natural language question.
func extractSearchKeywords(question string) string {
	// Remove common Korean particles and question markers.
	replacer := strings.NewReplacer(
		"은", " ", "는", " ", "이", " ", "가", " ",
		"을", " ", "를", " ", "에", " ", "의", " ",
		"로", " ", "으로", " ", "에서", " ", "와", " ",
		"과", " ", "도", " ", "까지", " ", "부터", " ",
		"하나요", " ", "인가요", " ", "인지", " ", "할까요", " ",
		"뭐야", " ", "뭔가요", " ", "무엇", " ",
		"어떻게", " ", "어떤", " ", "왜", " ",
		"?", " ", "？", " ", ".", " ",
	)
	cleaned := replacer.Replace(question)

	// Split and filter short/stop words.
	words := strings.Fields(cleaned)
	var keywords []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if len(w) < 2 {
			continue
		}
		keywords = append(keywords, w)
	}

	if len(keywords) == 0 {
		return question // fallback to full question
	}
	return strings.Join(keywords, " ")
}

// scoreAndSelectGuides scores all builtin guides by keyword relevance and returns the top N.
func scoreAndSelectGuides(keywords []string, maxGuides int) []guideEntry {
	if len(keywords) == 0 {
		return nil
	}

	type scored struct {
		guide guideEntry
		score int
	}

	var results []scored
	for _, key := range builtinGuideOrder {
		g := builtinGuides[key]
		score := scoreGuideRelevance(g, keywords)
		if score > 0 {
			results = append(results, scored{guide: g, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	var out []guideEntry
	for i, r := range results {
		if i >= maxGuides {
			break
		}
		out = append(out, r.guide)
	}
	return out
}

// scoreGuideRelevance scores a guide by keyword hits.
// Title hits weighted 3x, summary 2x, content 1x.
func scoreGuideRelevance(g guideEntry, keywords []string) int {
	titleLower := strings.ToLower(g.Title)
	summaryLower := strings.ToLower(g.Summary)
	contentLower := strings.ToLower(g.Content)

	score := 0
	for _, kw := range keywords {
		score += strings.Count(titleLower, kw) * 3
		score += strings.Count(summaryLower, kw) * 2
		score += strings.Count(contentLower, kw)
	}
	return score
}

// assembleAskContext builds the user message for the LLM with gathered context.
func assembleAskContext(question string, docs, guides, code []namedContent) string {
	var sb strings.Builder
	sb.WriteString("## Question\n")
	sb.WriteString(question)
	sb.WriteString("\n\n")

	totalChars := len(question)

	appendSources := func(heading string, sources []namedContent) {
		if len(sources) == 0 {
			return
		}
		sb.WriteString("## ")
		sb.WriteString(heading)
		sb.WriteString("\n\n")
		for _, s := range sources {
			if totalChars+len(s.content) > polarisAskMaxContextChars {
				remaining := polarisAskMaxContextChars - totalChars
				if remaining > 200 {
					fmt.Fprintf(&sb, "### %s\n%s\n\n", s.name, s.content[:remaining])
				}
				break
			}
			fmt.Fprintf(&sb, "### %s\n%s\n\n", s.name, s.content)
			totalChars += len(s.content)
		}
	}

	appendSources("Relevant System Guides", guides)
	appendSources("Relevant Documentation", docs)
	appendSources("Relevant Source Code", code)

	if len(guides) == 0 && len(docs) == 0 && len(code) == 0 {
		sb.WriteString("(No relevant context found for this question.)\n\n")
	}

	sb.WriteString("Answer the question based on the context above.")
	return sb.String()
}

// buildAskFallback formats gathered content when sglang is unavailable.
func buildAskFallback(question string, docs, guides, code []namedContent) string {
	var sb strings.Builder
	sb.WriteString("[polaris: sglang 서버에 연결할 수 없어 원본 검색 결과를 반환합니다]\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(question)

	for _, g := range guides {
		sb.WriteString("\n\n--- ")
		sb.WriteString(g.name)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateChars(g.content, 2000))
	}
	for _, d := range docs {
		sb.WriteString("\n\n--- ")
		sb.WriteString(d.name)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateChars(d.content, 2000))
	}
	for _, c := range code {
		sb.WriteString("\n\n--- ")
		sb.WriteString(c.name)
		sb.WriteString(" ---\n")
		sb.WriteString(truncateChars(c.content, 2000))
	}

	return sb.String()
}

// truncateChars truncates a string to maxChars, cutting at the last newline.
func truncateChars(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	cut := s[:maxChars]
	if idx := strings.LastIndex(cut, "\n"); idx > maxChars/2 {
		return cut[:idx] + "\n..."
	}
	return cut + "..."
}
