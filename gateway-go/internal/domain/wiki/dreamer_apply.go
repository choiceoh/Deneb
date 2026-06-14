// dreamer_apply.go — synthesis and application of a dream cycle: the LLM
// call that turns diary content into wikiUpdate proposals (synthesize) and
// the apply pass that writes/merges pages, rebuilds the index, and merges
// tags/related lists. Split from dreamer.go (WikiDreamer core).
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

type flexStringList []string

func (f *flexStringList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = nil
		return nil
	}
	switch trimmed[0] {
	case '[':
		var arr []string
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*f = arr
	case '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = splitFlexList(s)
	default:
		return fmt.Errorf("flexStringList: expected JSON array or string, got %.40s", trimmed)
	}
	return nil
}

// splitFlexList breaks a delimited string into trimmed, non-empty elements.
func splitFlexList(s string) flexStringList {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make(flexStringList, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// wikiUpdate represents a single page update instruction from the LLM.
type wikiUpdate struct {
	Action     string         `json:"action"` // "create" or "update"
	Path       string         `json:"path"`   // e.g., "기술/dgx-spark.md"
	Title      string         `json:"title"`
	ID         string         `json:"id"`      // short kebab-case identifier (e.g., "dgx-spark")
	Summary    string         `json:"summary"` // one-line description (~80 chars)
	Category   string         `json:"category"`
	Tags       flexStringList `json:"tags"`
	Related    flexStringList `json:"related"` // existing page paths semantically related
	Content    string         `json:"content"` // markdown body or section to append
	Importance float64        `json:"importance"`
	Type       string         `json:"type"`       // concept, entity, source, comparison, log
	Confidence string         `json:"confidence"` // high, medium, low
	Due        string         `json:"due"`        // YYYY-MM-DD upcoming deadline (거래 category)
	Supersedes string         `json:"supersedes"` // relPath of an existing page this update REPLACES (contradicted facts)
}

// synthesize calls the LLM to determine which wiki pages should be updated.
func (wd *WikiDreamer) synthesize(ctx context.Context, diaryContent string, state diaryProcessState) ([]wikiUpdate, error) {
	ctx, cancel := context.WithTimeout(ctx, wikiDreamSynthesisTimeout)
	defer cancel()

	// Build existing wiki context.
	idx := wd.store.Index()
	indexContent := idx.Render()
	processedHistory := formatProcessedDiaryCapsules(state.Recent)

	polarisSection := ""
	if wd.polarisContextFn != nil {
		if ctx := wd.polarisContextFn(); ctx != "" {
			polarisSection = "\n## 최근 Polaris 압축 요약 (사전 추출된 사실)\n" + ctx + "\n"
		}
	}

	prompt := fmt.Sprintf(`당신은 위키 지식베이스 관리자입니다. 아래 일지 내용을 분석하여 위키 페이지를 생성하거나 업데이트할 지시사항을 JSON 배열로 반환하세요.

## 현재 위키 인덱스
%s

## 최근 처리 이력
%s
%s
## 새 일지 내용
%s

## 규칙
- 일시적인 내용(인사, 잡담)은 무시
- 중요한 결정, 새로운 사실, 인물 정보, 프로젝트 진행 등만 위키에 반영
- 기존 페이지가 있으면 action:"update", 없으면 action:"create"
- 최근 처리 이력에 이미 반영된 주제/경로는 새 사실이 추가된 경우에만 update하고, 같은 내용을 반복 생성하지 마라
- 카테고리: 사람, 프로젝트, 거래, 기술, 업무, 결정, 선호
- 거래 카테고리: 거래처·금액·납기가 걸린 건별 트랜잭션. 가장 임박한 결제기한/마감일은 frontmatter의 due 필드(YYYY-MM-DD)에 기록
- content는 마크다운 형식. create 시 전체 본문, update 시 추가할 섹션/내용. 본문에서 다른 페이지를 언급할 때는 [[경로-또는-제목]] 형식의 위키링크를 쓰면 지식그래프 엣지가 된다 (예: [[프로젝트/dgx-spark]], [[홍길동]])
- importance: 0.5(일반) ~ 0.9(핵심 결정)
- type: 페이지 유형 — concept(개념), entity(인물/조직), source(출처), comparison(비교), log(이력)
- confidence: 정보 신뢰도 — high(검증됨), medium(합리적 추론), low(불확실)
- due: 거래의 임박한 결제기한·마감일 (YYYY-MM-DD). 거래 카테고리에서만 사용, 없으면 생략
- supersedes: 새 일지 내용이 기존 페이지의 사실과 **모순되거나 그것을 대체**할 때, 대체되는 기존 페이지 경로 (인덱스에서 선택). 단순 추가 정보면 생략 — 사실이 바뀐 경우에만 (예: 단가 변경, 담당자 교체, 정책 폐기)
- id: 짧은 kebab-case 식별자 (예: "dgx-spark", "gemma4-switch", "peter-kim")
- summary: 한 줄 요약 (~80자, 한국어)
- related: 의미적으로 관련된 기존 위키 페이지 경로 목록 (인덱스에서 선택)
- 업데이트가 불필요하면 빈 배열 [] 반환

JSON 배열만 반환하세요. 다른 텍스트 없이.`, indexContent, processedHistory, polarisSection, diaryContent)

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

	// Defense in depth: even if Site 1 (transcript) redacted raw tool output,
	// the LLM may still paraphrase or quote a secret into its wiki synthesis
	// ("the user's API key starts with sk-proj…"). Redact every free-text
	// field on the proposed updates before they flow into the store.
	for i := range updates {
		updates[i].Title = redact.String(updates[i].Title)
		updates[i].Summary = redact.String(updates[i].Summary)
		updates[i].Content = redact.String(updates[i].Content)
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
		// The LLM occasionally wraps its proposed content in a frontmatter
		// block; strip it here so the append/create paths below never fold a
		// second frontmatter into the page body. (Store.WritePage strips the
		// create case too, but the update-append at existing.Body += u.Content
		// would otherwise embed it mid-body, out of that helper's reach.)
		u.Content = StripLeadingFrontmatter(u.Content)
		if !strings.HasSuffix(u.Path, ".md") {
			u.Path += ".md"
		}
		// Strip a wikilink namespace ("w:") the model sometimes prefixes onto the
		// path's category directory (e.g. "w:프로젝트/…"). Categories are the page's
		// directory (Store.Stats uses filepath.Dir), so a "w:프로젝트/" path files the
		// page under a phantom "w:프로젝트" that duplicates the real "프로젝트" in the
		// browser and, sharing a title, slips past the dedup below. Normalizing here
		// folds both away and lets the dedup catch the duplicate.
		u.Path = normalizeWikiPath(u.Path)
		// Validate category; remap invalid ones to "운영시스템" as fallback.
		if u.Category != "" && !ValidateCategory(u.Category) {
			wd.logger.Warn("wiki-dream: invalid category, remapping to 운영시스템",
				"category", u.Category, "path", u.Path)
			u.Category = "운영시스템"
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
			if u.Due != "" {
				page.Meta.Due = u.Due
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
				if u.Due != "" {
					page.Meta.Due = u.Due
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
			if u.Due != "" {
				existing.Meta.Due = u.Due
			}
			existing.Meta.Updated = time.Now().Format("2006-01-02")

			if err := wd.store.WritePage(u.Path, existing); err != nil {
				wd.logger.Warn("wiki-dream: update page failed", "path", u.Path, "error", err)
				continue
			}
			updated++
		}

		// Contradiction handling: when the LLM flagged this update as
		// REPLACING an existing page's facts, stamp the old page so search
		// demotes it (the page itself stays readable — history is memory too).
		if u.Supersedes != "" {
			if err := wd.store.MarkSuperseded(u.Supersedes, u.Path); err != nil {
				wd.logger.Warn("wiki-dream: supersede mark failed",
					"old", u.Supersedes, "new", u.Path, "error", err)
			} else {
				wd.logger.Info("wiki-dream: page superseded", "old", u.Supersedes, "new", u.Path)
			}
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

// normalizeWikiPath strips a leading wikilink namespace ("w:") from a proposed
// page path. The dreamer model occasionally prefixes the path's category
// directory with the knowledge-router's "w:" ref form ("w:프로젝트/…"); since the
// category is the page's directory, that files the page under a phantom
// "w:프로젝트" bucket that duplicates "프로젝트". A plain path is unchanged.
func normalizeWikiPath(p string) string {
	return strings.TrimPrefix(strings.TrimSpace(p), "w:")
}

func (wd *WikiDreamer) resetCounters() {
	wd.cmu.Lock()
	wd.turnCount = 0
	wd.lastDream = time.Now()
	last := wd.lastDream
	wd.cmu.Unlock()
	// Persist lastDream so the time-trigger survives restarts (see NewWikiDreamer).
	if wd.store == nil {
		return
	}
	state := wd.loadDiaryProcessState()
	state.LastDreamMs = last.UnixMilli()
	if err := wd.saveDiaryProcessState(state); err != nil && wd.logger != nil {
		wd.logger.Warn("wiki-dream: persist lastDream failed", "error", err)
	}
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
