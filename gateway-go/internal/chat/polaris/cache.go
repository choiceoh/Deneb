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
	polarisAskPerSourceChars  = 4000  // max chars per individual source
)

const polarisAskSystemPrompt = `You are Polaris, Deneb's internal documentation expert.
Given a question and relevant documentation/guides, provide a direct, comprehensive answer.
Rules:
- Answer in the same language as the question (Korean if Korean, English if English).
- Be precise and cite specific docs/guides when relevant.
- Output for Telegram (max 3500 chars). Be concise but complete.
- Use markdown formatting sparingly (bold for key terms, code blocks for code).
- If the documentation doesn't cover the topic, say so clearly.
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

// NewHandler returns the polaris tool handler function for use with ToolRegistry.
// This handler supports all actions except ask (no LLM dependency).
func NewHandler(workspaceDir string) func(context.Context, json.RawMessage) (string, error) {
	return newHandler(workspaceDir, nil, nil)
}

// NewHandlerWithLLM returns the polaris tool handler with AI-powered ask action.
// The llmFn and healthFn are injected from the chat package to avoid circular imports.
func NewHandlerWithLLM(workspaceDir string, llmFn LLMSynthesizer, healthFn HealthChecker) func(context.Context, json.RawMessage) (string, error) {
	return newHandler(workspaceDir, llmFn, healthFn)
}

func newHandler(workspaceDir string, llmFn LLMSynthesizer, healthFn HealthChecker) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		docsDir := resolveDocsDir(workspaceDir)
		var p struct {
			Action   string `json:"action"`
			Query    string `json:"query"`
			Topic    string `json:"topic"`
			Question string `json:"question"`
		}
		if err := jsonutil.UnmarshalInto("polaris params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "ask":
			return polarisAsk(ctx, docsDir, p.Question, llmFn, healthFn)
		case "topics":
			return polarisTopics(docsDir, p.Topic)
		case "search":
			return polarisSearch(docsDir, p.Query)
		case "read":
			return polarisRead(docsDir, p.Topic)
		case "guides":
			return polarisGuides(p.Topic)
		default:
			return "", fmt.Errorf("unknown action %q (valid: ask, topics, search, read, guides)", p.Action)
		}
	}
}

// --- ask action ---

// polarisAsk autonomously gathers relevant docs/guides and synthesizes an answer.
func polarisAsk(ctx context.Context, docsDir, question string, llmFn LLMSynthesizer, healthFn HealthChecker) (string, error) {
	if question == "" {
		return "", fmt.Errorf("question is required for ask action")
	}

	// Phase 1: Gather relevant knowledge.
	keywords := extractSearchKeywords(question)

	// Search docs and guides by keywords.
	searchResults := polarisSearchInternal(docsDir, keywords)

	// Score guides by relevance independently (covers guides that match partially).
	kwList := strings.Fields(strings.ToLower(keywords))
	topGuides := scoreAndSelectGuides(kwList, polarisAskMaxGuides)

	// Read top doc matches in full.
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
			name:    m.Path,
			content: truncateChars(body, polarisAskPerSourceChars),
		})
		if len(docContents) >= polarisAskMaxDocs {
			break
		}
	}

	// Collect guide content.
	var guideContents []namedContent
	for _, g := range topGuides {
		guideContents = append(guideContents, namedContent{
			name:    g.Key + " (guide)",
			content: truncateChars(g.Content, polarisAskPerSourceChars),
		})
	}

	// Phase 2: Assemble context for LLM.
	contextText := assembleAskContext(question, docContents, guideContents)

	// Phase 3: Check LLM availability and synthesize.
	if llmFn == nil || healthFn == nil || !healthFn() {
		return buildAskFallback(question, docContents, guideContents), nil
	}

	result, err := llmFn(ctx, polarisAskSystemPrompt, contextText, polarisAskMaxTokens)
	if err != nil {
		return buildAskFallback(question, docContents, guideContents), nil
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
func assembleAskContext(question string, docs, guides []namedContent) string {
	var sb strings.Builder
	sb.WriteString("## Question\n")
	sb.WriteString(question)
	sb.WriteString("\n\n")

	totalChars := len(question)

	if len(guides) > 0 {
		sb.WriteString("## Relevant System Guides\n\n")
		for _, g := range guides {
			if totalChars+len(g.content) > polarisAskMaxContextChars {
				remaining := polarisAskMaxContextChars - totalChars
				if remaining > 200 {
					fmt.Fprintf(&sb, "### %s\n%s\n\n", g.name, g.content[:remaining])
				}
				break
			}
			fmt.Fprintf(&sb, "### %s\n%s\n\n", g.name, g.content)
			totalChars += len(g.content)
		}
	}

	if len(docs) > 0 {
		sb.WriteString("## Relevant Documentation\n\n")
		for _, d := range docs {
			if totalChars+len(d.content) > polarisAskMaxContextChars {
				remaining := polarisAskMaxContextChars - totalChars
				if remaining > 200 {
					fmt.Fprintf(&sb, "### %s\n%s\n\n", d.name, d.content[:remaining])
				}
				break
			}
			fmt.Fprintf(&sb, "### %s\n%s\n\n", d.name, d.content)
			totalChars += len(d.content)
		}
	}

	if len(guides) == 0 && len(docs) == 0 {
		sb.WriteString("(No relevant documentation found for this question.)\n\n")
	}

	sb.WriteString("Answer the question based on the documentation above.")
	return sb.String()
}

// buildAskFallback formats gathered content when sglang is unavailable.
func buildAskFallback(question string, docs, guides []namedContent) string {
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
