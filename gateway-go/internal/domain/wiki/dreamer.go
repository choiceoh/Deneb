// dreamer.go — WikiDreamer: implements autonomous.Dreamer for wiki-based
// memory consolidation. Instead of SQL-based fact verification/merging,
// it scans diary entries and synthesizes them into wiki pages via LLM.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Dreaming configuration.
const (
	wikiDreamTurnThreshold = 50
	wikiDreamTimeIntervalH = 8
	wikiDreamTimeout       = 10 * time.Minute
	wikiDreamMaxTokens     = 4096
)

// Compile-time interface compliance.
var _ autonomous.Dreamer = (*WikiDreamer)(nil)

// WikiDreamer implements autonomous.Dreamer for wiki-based knowledge consolidation.
// Phases:
//  1. Scan unprocessed diary entries
//  2. LLM synthesis: identify which wiki pages to create/update
//  3. Apply page updates
//  4. Rebuild index
type WikiDreamer struct {
	store  *Store
	config Config
	client *llm.Client
	model  string
	logger *slog.Logger

	turnCount int
	lastDream time.Time
}

// NewWikiDreamer creates a new wiki dreamer.
func NewWikiDreamer(store *Store, client *llm.Client, model string, cfg Config, logger *slog.Logger) *WikiDreamer {
	return &WikiDreamer{
		store:  store,
		config: cfg,
		client: client,
		model:  model,
		logger: logger,
	}
}

// IncrementTurn records a conversation turn for threshold tracking.
func (wd *WikiDreamer) IncrementTurn(_ context.Context) {
	wd.turnCount++
}

// ShouldDream checks if dreaming conditions are met.
func (wd *WikiDreamer) ShouldDream(_ context.Context) bool {
	if wd.turnCount >= wikiDreamTurnThreshold {
		wd.logger.Info("wiki-dream: turn threshold reached", "turns", wd.turnCount)
		return true
	}
	if !wd.lastDream.IsZero() && time.Since(wd.lastDream).Hours() >= float64(wikiDreamTimeIntervalH) {
		wd.logger.Info("wiki-dream: time threshold reached", "elapsed", time.Since(wd.lastDream).Round(time.Minute))
		return true
	}
	return false
}

// RunDream executes the wiki consolidation cycle.
func (wd *WikiDreamer) RunDream(ctx context.Context) (*autonomous.DreamReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, wikiDreamTimeout)
	defer cancel()

	report := &autonomous.DreamReport{}
	var phaseErrors []string

	// Phase 1: Scan unprocessed diary entries.
	diaryContent, err := wd.scanDiaries(ctx)
	if err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("diary-scan: %v", err))
	}

	if diaryContent == "" {
		wd.logger.Info("wiki-dream: no new diary entries to process")
		wd.resetCounters()
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	// Phase 2: LLM synthesis — determine which wiki pages to update.
	if wd.client == nil {
		phaseErrors = append(phaseErrors, "synthesis: LLM client not available")
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	updates, err := wd.synthesize(ctx, diaryContent)
	if err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("synthesis: %v", err))
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	// Phase 3: Apply page updates.
	created, updated, oversized := wd.applyUpdates(ctx, updates)
	report.WikiPagesCreated = created
	report.WikiPagesUpdated = updated
	if len(oversized) > 0 {
		phaseErrors = append(phaseErrors, fmt.Sprintf("oversized pages: %s", strings.Join(oversized, ", ")))
	}

	// Phase 4: Rebuild index.
	if err := wd.rebuildIndex(); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-rebuild: %v", err))
	}

	// Phase 5: Verify existing pages (duplicate detection + misclassification).
	findings := wd.verifyPages(ctx)
	if len(findings) > 0 {
		for _, f := range findings {
			report.VerifyFindings = append(report.VerifyFindings, f.Detail)
		}
		wd.logger.Info("wiki-dream: verification findings", "count", len(findings))
	}

	// Update last-processed diary date in index.
	idx := wd.store.Index()
	idx.LastProcessed = time.Now().Format("2006-01-02")
	indexPath := filepath.Join(wd.store.Dir(), "index.md")
	if err := idx.Save(indexPath); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-save: %v", err))
	}

	wd.resetCounters()
	report.PhaseErrors = phaseErrors
	report.DurationMs = time.Since(start).Milliseconds()

	wd.logger.Info("wiki-dream: cycle complete",
		"created", created, "updated", updated,
		"duration", time.Since(start).Round(time.Millisecond))

	return report, nil
}

// scanDiaries reads diary entries since the last processed date.
func (wd *WikiDreamer) scanDiaries(_ context.Context) (string, error) {
	diaryDir := wd.store.DiaryDir()
	if diaryDir == "" {
		return "", nil
	}

	entries, err := os.ReadDir(diaryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read diary dir: %w", err)
	}

	// Determine cutoff date from index.
	idx := wd.store.Index()
	cutoff := idx.LastProcessed

	var diaryFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "diary-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// Extract date from filename: diary-YYYY-MM-DD.md
		date := strings.TrimPrefix(e.Name(), "diary-")
		date = strings.TrimSuffix(date, ".md")
		if cutoff != "" && date <= cutoff {
			continue
		}
		diaryFiles = append(diaryFiles, e.Name())
	}

	if len(diaryFiles) == 0 {
		return "", nil
	}

	sort.Strings(diaryFiles)

	// Read and concatenate diary content (cap at 30K chars).
	var sb strings.Builder
	const maxChars = 30000
	for _, name := range diaryFiles {
		data, err := os.ReadFile(filepath.Join(diaryDir, name))
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "--- %s ---\n", name)
		sb.Write(data)
		sb.WriteByte('\n')
		if sb.Len() > maxChars {
			break
		}
	}

	return sb.String(), nil
}

// wikiUpdate represents a single page update instruction from the LLM.
type wikiUpdate struct {
	Action     string   `json:"action"` // "create" or "update"
	Path       string   `json:"path"`   // e.g., "기술/dgx-spark.md"
	Title      string   `json:"title"`
	ID         string   `json:"id"`      // short kebab-case identifier (e.g., "dgx-spark")
	Summary    string   `json:"summary"` // one-line description (~80 chars)
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	Related    []string `json:"related"` // existing page paths semantically related
	Content    string   `json:"content"` // markdown body or section to append
	Importance float64  `json:"importance"`
	Type       string   `json:"type"`       // concept, entity, source, comparison, log
	Confidence string   `json:"confidence"` // high, medium, low
}

// synthesize calls the LLM to determine which wiki pages should be updated.
func (wd *WikiDreamer) synthesize(ctx context.Context, diaryContent string) ([]wikiUpdate, error) {
	// Build existing wiki context.
	idx := wd.store.Index()
	indexContent := idx.Render()

	prompt := fmt.Sprintf(`당신은 위키 지식베이스 관리자입니다. 아래 일지 내용을 분석하여 위키 페이지를 생성하거나 업데이트할 지시사항을 JSON 배열로 반환하세요.

## 현재 위키 인덱스
%s

## 새 일지 내용
%s

## 규칙
- 일시적인 내용(인사, 잡담)은 무시
- 중요한 결정, 새로운 사실, 인물 정보, 프로젝트 진행 등만 위키에 반영
- 기존 페이지가 있으면 action:"update", 없으면 action:"create"
- 카테고리: 사람, 프로젝트, 기술, 업무, 결정, 선호
- content는 마크다운 형식. create 시 전체 본문, update 시 추가할 섹션/내용
- importance: 0.5(일반) ~ 0.9(핵심 결정)
- type: 페이지 유형 — concept(개념), entity(인물/조직), source(출처), comparison(비교), log(이력)
- confidence: 정보 신뢰도 — high(검증됨), medium(합리적 추론), low(불확실)
- id: 짧은 kebab-case 식별자 (예: "dgx-spark", "gemma4-switch", "peter-kim")
- summary: 한 줄 요약 (~80자, 한국어)
- related: 의미적으로 관련된 기존 위키 페이지 경로 목록 (인덱스에서 선택)
- 업데이트가 불필요하면 빈 배열 [] 반환

JSON 배열만 반환하세요. 다른 텍스트 없이.`, indexContent, diaryContent)

	systemJSON, _ := json.Marshal("You are a wiki knowledge base maintainer. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: wikiDreamMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	// Extract JSON from response.
	text := resp
	text = strings.TrimSpace(text)

	// Strip markdown code fences if present.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	var updates []wikiUpdate
	if err := json.Unmarshal([]byte(text), &updates); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w (raw: %.200s)", err, text)
	}

	return updates, nil
}

// applyUpdates creates or updates wiki pages based on LLM instructions.
// Returns (created, updated) counts and paths of oversized pages.
func (wd *WikiDreamer) applyUpdates(_ context.Context, updates []wikiUpdate) (created, updated int, oversized []string) {
	maxBytes := wd.config.MaxPageBytes

	for _, u := range updates {
		if u.Path == "" || u.Title == "" {
			continue
		}
		if !strings.HasSuffix(u.Path, ".md") {
			u.Path += ".md"
		}
		// Validate category; remap invalid ones to "업무" as fallback.
		if u.Category != "" && !ValidateCategory(u.Category) {
			wd.logger.Warn("wiki-dream: invalid category, remapping to 업무",
				"category", u.Category, "path", u.Path)
			u.Category = "업무"
			// Fix path prefix to match corrected category.
			parts := strings.SplitN(u.Path, "/", 2)
			if len(parts) == 2 {
				u.Path = u.Category + "/" + parts[1]
			}
		}

		// Duplicate prevention: if creating, check for existing similar pages.
		if u.Action == "create" {
			if existing := wd.findExistingPage(u); existing != "" {
				wd.logger.Info("wiki-dream: duplicate detected, converting to update",
					"proposed", u.Path, "existing", existing)
				u.Action = "update"
				u.Path = existing
			}
		}

		switch u.Action {
		case "create":
			page := NewPage(u.Title, u.Category, u.Tags)
			if u.Importance > 0 {
				page.Meta.Importance = u.Importance
			}
			if u.ID != "" {
				page.Meta.ID = u.ID
			}
			if u.Summary != "" {
				page.Meta.Summary = u.Summary
			}
			if len(u.Related) > 0 {
				page.Meta.Related = u.Related
			}
			if u.Type != "" {
				page.Meta.Type = u.Type
			}
			if u.Confidence != "" {
				page.Meta.Confidence = u.Confidence
			}
			if u.Content != "" {
				page.Body = u.Content
			} else {
				page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성 (dreaming)\n",
					u.Title, time.Now().Format("2006-01-02"))
			}
			// Append a related-docs section if related pages are provided.
			if len(u.Related) > 0 {
				page.Body += "\n\n## 관련 문서\n"
				for _, r := range u.Related {
					page.Body += fmt.Sprintf("- [[%s]]\n", r)
				}
			}
			if err := wd.store.WritePage(u.Path, page); err != nil {
				wd.logger.Warn("wiki-dream: create page failed", "path", u.Path, "error", err)
				continue
			}
			created++

		case "update":
			existing, err := wd.store.ReadPage(u.Path)
			if err != nil {
				// Page doesn't exist — create it instead.
				page := NewPage(u.Title, u.Category, u.Tags)
				if u.Importance > 0 {
					page.Meta.Importance = u.Importance
				}
				if u.ID != "" {
					page.Meta.ID = u.ID
				}
				if u.Summary != "" {
					page.Meta.Summary = u.Summary
				}
				if len(u.Related) > 0 {
					page.Meta.Related = u.Related
				}
				if u.Type != "" {
					page.Meta.Type = u.Type
				}
				if u.Confidence != "" {
					page.Meta.Confidence = u.Confidence
				}
				page.Body = u.Content
				if err := wd.store.WritePage(u.Path, page); err != nil {
					wd.logger.Warn("wiki-dream: create-on-update failed", "path", u.Path, "error", err)
					continue
				}
				created++
				continue
			}

			// Append content to existing page.
			if u.Content != "" {
				existing.Body += "\n\n" + u.Content
			}
			if len(u.Tags) > 0 {
				existing.Meta.Tags = mergeTags(existing.Meta.Tags, u.Tags)
			}
			if u.Importance > existing.Meta.Importance {
				existing.Meta.Importance = u.Importance
			}
			if u.ID != "" {
				existing.Meta.ID = u.ID
			}
			if u.Summary != "" {
				existing.Meta.Summary = u.Summary
			}
			if len(u.Related) > 0 {
				existing.Meta.Related = mergeRelated(existing.Meta.Related, u.Related)
			}
			if u.Type != "" {
				existing.Meta.Type = u.Type
			}
			if u.Confidence != "" {
				existing.Meta.Confidence = u.Confidence
			}
			existing.Meta.Updated = time.Now().Format("2006-01-02")

			if err := wd.store.WritePage(u.Path, existing); err != nil {
				wd.logger.Warn("wiki-dream: update page failed", "path", u.Path, "error", err)
				continue
			}
			updated++
		}

		// Check page size and split if needed.
		if maxBytes > 0 {
			abs := filepath.Join(wd.store.Dir(), u.Path)
			if info, err := os.Stat(abs); err == nil && info.Size() > int64(maxBytes) {
				subPaths, splitErr := wd.store.SplitPage(u.Path, maxBytes)
				if splitErr != nil {
					wd.logger.Warn("wiki-dream: split failed",
						"path", u.Path, "error", splitErr)
					oversized = append(oversized, u.Path)
				} else if len(subPaths) > 0 {
					wd.logger.Info("wiki-dream: page split",
						"path", u.Path, "subPages", len(subPaths))
					created += len(subPaths)
				} else {
					wd.logger.Warn("wiki-dream: page oversized but cannot split",
						"path", u.Path, "size", info.Size())
					oversized = append(oversized, u.Path)
				}
			}
		}
	}

	return created, updated, oversized
}

// rebuildIndex scans all wiki pages and rebuilds the master index.
func (wd *WikiDreamer) rebuildIndex() error {
	pages, err := wd.store.ListPages("")
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	idx := wd.store.Index()
	// Preserve LastProcessed from the old index.
	lastProcessed := idx.LastProcessed

	newIdx := NewIndex()
	newIdx.LastProcessed = lastProcessed

	for _, relPath := range pages {
		page, err := wd.store.ReadPage(relPath)
		if err != nil {
			continue
		}
		newIdx.UpdateEntry(relPath, page)
	}

	wd.store.mu.Lock()
	wd.store.index = newIdx
	err = newIdx.Save(filepath.Join(wd.store.Dir(), "index.md"))
	wd.store.mu.Unlock()

	return err
}

// findExistingPage checks if a similar page already exists by ID match,
// slug prefix match, or FTS title search. Returns the existing path or "".
func (wd *WikiDreamer) findExistingPage(u wikiUpdate) string {
	idx := wd.store.Index()

	// 1. Exact ID match in the same category.
	if u.ID != "" {
		for path, entry := range idx.Entries {
			if entry.ID == u.ID {
				return path
			}
		}
	}

	// 2. Slug prefix match: normalize both and compare.
	proposedSlug := normalizeSlug(u.Path)
	for path := range idx.Entries {
		if normalizeSlug(path) == proposedSlug {
			return path
		}
	}

	// 3. FTS title search: if a result in the same category scores well.
	if u.Title != "" && wd.store.fts != nil {
		results, err := wd.store.fts.search(context.Background(), u.Title, 3)
		if err == nil {
			for _, r := range results {
				if r.Score < 0.6 {
					continue
				}
				// Same category check.
				if u.Category != "" && strings.HasPrefix(r.Path, u.Category+"/") {
					return r.Path
				}
			}
		}
	}

	return ""
}

// normalizeSlug reduces a wiki path to a comparable slug form.
// "사람/에코프로-담당자---석문호,-표과장.md" -> "사람/에코프로담당자석문호표과장"
func normalizeSlug(path string) string {
	path = strings.TrimSuffix(path, ".md")
	path = strings.ToLower(path)
	var sb strings.Builder
	for _, r := range path {
		if r == '/' {
			sb.WriteRune(r)
		} else if r == '-' || r == '_' || r == ',' || r == ' ' || r == '(' || r == ')' {
			continue
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func (wd *WikiDreamer) resetCounters() {
	wd.turnCount = 0
	wd.lastDream = time.Now()
}

// mergeTags merges two tag lists, deduplicating.
func mergeTags(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, t := range existing {
		seen[t] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, t := range added {
		if _, ok := seen[t]; !ok {
			result = append(result, t)
			seen[t] = struct{}{}
		}
	}
	return result
}

// mergeRelated merges two related-page lists, deduplicating (union).
func mergeRelated(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, r := range existing {
		seen[r] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, r := range added {
		if _, ok := seen[r]; !ok {
			result = append(result, r)
			seen[r] = struct{}{}
		}
	}
	return result
}
