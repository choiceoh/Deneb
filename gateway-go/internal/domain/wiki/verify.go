// verify.go — Wiki verification: duplicate entity detection + misclassification check.
// Called as Phase 5 of the WikiDreamer cycle. Detection only — no auto-fix.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// VerifyFinding represents a single verification issue found.
type VerifyFinding struct {
	Type   string `json:"type"`            // "duplicate", "misclassified", or "stale_deadline"
	Detail string `json:"detail"`          // human-readable description (Korean)
	PageA  string `json:"pageA"`           // primary page path
	PageB  string `json:"pageB,omitempty"` // secondary page path (for duplicates)
	// Fix is set only on HIGH-CONFIDENCE findings the dream cycle may auto-apply
	// (Phase 5): an exact-duplicate merge or an LLM-high-confidence category move.
	// Nil means advisory-only — surfaced in the report, never auto-touched.
	Fix *VerifyFix `json:"fix,omitempty"`
}

// VerifyFix is the structured, auto-applicable correction attached to a
// high-confidence VerifyFinding. Conservative by construction: only the two
// safe, reversible (git-recoverable) actions are expressible.
type VerifyFix struct {
	Kind    string `json:"kind"`              // "merge" (fold PageB into PageA, delete PageB) | "move" (PageA → NewPath)
	NewPath string `json:"newPath,omitempty"` // move: the corrected path under the right category
}

// verifyPages runs verification on existing wiki pages:
//  1. Duplicate detection via Levenshtein distance on titles/IDs (no LLM)
//  2. Misclassification detection via single LLM call
//  3. Stale-deadline detection: pages whose `due` date has already passed (no LLM)
//
// Detection only — no auto-fix. Stale deadlines are surfaced, never deleted, so
// the operator (or a later analysis turn) decides whether a deal/milestone is
// done or slipped, and analysis stops treating a passed deadline as upcoming.
func (wd *WikiDreamer) verifyPages(ctx context.Context) []VerifyFinding {
	idx := wd.store.Index()
	if len(idx.Entries) < 2 {
		return nil
	}

	var findings []VerifyFinding

	// 5a: Duplicate detection (pure computation).
	findings = append(findings, detectDuplicates(idx)...)

	// 5b: Misclassification detection (single LLM call).
	if wd.client != nil {
		findings = append(findings, wd.detectMisclassifications(ctx, idx)...)
	}

	// 5c: Stale-deadline detection (pure computation).
	findings = append(findings, wd.detectStaleDeadlines()...)

	// 5d: Long-superseded pages get archived (pure computation). Supersession is
	// a soft flag — without this, superseded zombies pile up in search/index
	// forever (they were a third of the 2026-07 duplicate mess's long tail).
	findings = append(findings, wd.detectStaleSuperseded()...)

	return findings
}

// enrichRelatedLinks adds semantic `related` links to pages that currently have
// none, via Store.SuggestRelated (high cosine floor). Conservative by design:
// only zero-related pages, at most maxEnrichPerPage each, additive only (never
// removes). Returns the number of links added. No-op without an embedder.
func (wd *WikiDreamer) enrichRelatedLinks(ctx context.Context) int {
	const maxEnrichPerPage = 2
	if wd.store == nil || wd.store.sem == nil {
		return 0
	}
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return 0
	}
	added := 0
	for _, rp := range relPaths {
		page, perr := wd.store.ReadPage(rp)
		if perr != nil || page == nil || len(page.Meta.Related) > 0 {
			continue
		}
		sugg := wd.store.SuggestRelated(ctx, rp, maxEnrichPerPage)
		if len(sugg) == 0 {
			continue
		}
		// Apply via UpdatePage so a concurrent writer of rp can't be clobbered by
		// this Related-only edit. SuggestRelated (an embedding query) ran above,
		// outside the write lock. Re-check Related under the lock: another writer
		// may have filled it since the read, in which case skip (additive-only).
		written := false
		werr := wd.store.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil || len(cur.Meta.Related) > 0 {
				return nil, nil
			}
			cur.Meta.Related = sugg
			written = true
			return cur, nil
		})
		if werr != nil {
			wd.logger.Warn("wiki-dream: enrich write failed", "path", rp, "error", werr)
			continue
		}
		if written {
			added += len(sugg)
		}
	}
	return added
}

// detectStaleDeadlines flags pages whose frontmatter `due` (YYYY-MM-DD) is in
// the past. Reads pages directly because the index doesn't carry the due field.
// Pure computation, no LLM.
func (wd *WikiDreamer) detectStaleDeadlines() []VerifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	today := time.Now()
	var findings []VerifyFinding
	for _, rp := range relPaths {
		page, err := wd.store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		due := strings.TrimSpace(page.Meta.Due)
		if due == "" {
			continue
		}
		dueTime, perr := time.Parse("2006-01-02", due)
		if perr != nil {
			continue
		}
		days := int(today.Sub(dueTime).Hours() / 24)
		if days <= 0 {
			continue // deadline is today or still upcoming
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		findings = append(findings, VerifyFinding{
			Type:   "stale_deadline",
			Detail: fmt.Sprintf("기한 지남: %q (기한 %s, %d일 경과) — 처리 완료/갱신 필요", title, due, days),
			PageA:  rp,
		})
	}
	return findings
}

// staleSupersededAfterDays is how long a superseded page stays merely demoted
// before the verify pass archives it outright.
const staleSupersededAfterDays = 30

// detectStaleSuperseded flags pages that have carried a SupersededBy marker for
// over staleSupersededAfterDays without being touched — attach an auto-archive
// fix (reversible: the flag flips back, git keeps history).
func (wd *WikiDreamer) detectStaleSuperseded() []VerifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -staleSupersededAfterDays).Format("2006-01-02")
	var findings []VerifyFinding
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp) // ListPages walks with the OS separator
		page, err := wd.store.ReadPage(rp)
		if err != nil || page == nil || page.Meta.Archived || page.Meta.SupersededBy == "" {
			continue
		}
		last := strings.TrimSpace(page.Meta.Updated)
		if last == "" {
			last = strings.TrimSpace(page.Meta.Created)
		}
		if last == "" || last >= cutoff { // ISO dates compare lexicographically
			continue
		}
		findings = append(findings, VerifyFinding{
			Type: "stale_superseded",
			Detail: fmt.Sprintf("%s 이후 방치된 superseded 페이지 (→ %s) — 아카이브",
				last, page.Meta.SupersededBy),
			PageA: rp,
			Fix:   &VerifyFix{Kind: "archive"},
		})
	}
	return findings
}

type pageRef struct {
	path  string
	title string
	id    string
}

// detectDuplicates finds pages with identical or very similar titles/IDs.
func detectDuplicates(idx *Index) []VerifyFinding {
	pages := make([]pageRef, 0, len(idx.Entries))
	for path, entry := range idx.Entries {
		pages = append(pages, pageRef{path: path, title: entry.Title, id: entry.ID})
	}

	var findings []VerifyFinding
	seen := map[string]struct{}{}

	for i := 0; i < len(pages); i++ {
		for j := i + 1; j < len(pages); j++ {
			a, b := pages[i], pages[j]
			key := a.path + "|" + b.path

			if _, ok := seen[key]; ok {
				continue
			}

			// Compare titles. Normalized-key equality ("영산고 태양광" vs
			// "영산고-태양광" vs "영산고태양광") is as safe to auto-merge as an exact
			// match — punctuation/spacing variants are exactly how the same topic
			// splinters across agent writes.
			if a.title != "" && b.title != "" {
				if norm := normalizeTitleKey(a.title); norm != "" && norm == normalizeTitleKey(b.title) {
					findings = append(findings, exactDupFinding(idx, a.path, b.path,
						fmt.Sprintf("동일한 제목(정규화): \"%s\" ~ \"%s\"", a.title, b.title)))
					seen[key] = struct{}{}
					continue
				}
				if isSimilar(a.title, b.title) {
					findings = append(findings, VerifyFinding{
						Type: "duplicate",
						Detail: fmt.Sprintf("유사한 제목: \"%s\" ~ \"%s\" (거리 %d)",
							a.title, b.title, levenshtein(a.title, b.title)),
						PageA: a.path, PageB: b.path,
					})
					seen[key] = struct{}{}
					continue
				}
			}

			// Compare IDs.
			if _, dup := seen[key]; a.id != "" && b.id != "" && !dup && isSimilar(a.id, b.id) {
				dist := levenshtein(a.id, b.id)
				if dist == 0 {
					findings = append(findings, exactDupFinding(idx, a.path, b.path,
						fmt.Sprintf("동일한 ID: \"%s\"", a.id)))
				} else {
					findings = append(findings, VerifyFinding{
						Type:   "duplicate",
						Detail: fmt.Sprintf("유사한 ID: \"%s\" ~ \"%s\" (거리 %d)", a.id, b.id, dist),
						PageA:  a.path, PageB: b.path,
					})
				}
				seen[key] = struct{}{}
			}
		}
	}

	return findings
}

// normalizeTitleKey reduces a title to a comparison key: lowercase, keeping
// only letters and digits (spaces, hyphens, punctuation, brackets dropped).
// Two titles sharing a key are the same name spelled differently.
func normalizeTitleKey(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// isSimilar checks if two strings are similar enough to be potential duplicates.
func isSimilar(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	dist := levenshtein(a, b)
	if dist == 0 {
		return true
	}
	maxLen := max(utf8.RuneCountInString(a), utf8.RuneCountInString(b))
	if maxLen <= 5 {
		return dist <= 1
	}
	return dist <= 2 && float64(dist)/float64(maxLen) < 0.3
}

// misclassificationResult is the LLM response format for category errors.
type misclassificationResult struct {
	Path            string `json:"path"`
	CurrentCategory string `json:"currentCategory"`
	CorrectCategory string `json:"correctCategory"`
	Confidence      string `json:"confidence"` // high | medium | low — only "high" is auto-applied
	Reason          string `json:"reason"`
}

// detectMisclassifications sends page list to LLM to find category errors.
func (wd *WikiDreamer) detectMisclassifications(ctx context.Context, idx *Index) []VerifyFinding {
	var lines []string
	for path, entry := range idx.Entries {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s",
			path, entry.Title, entry.Category, entry.Summary))
	}
	if len(lines) == 0 {
		return nil
	}

	prompt := fmt.Sprintf(`아래는 위키 페이지 목록입니다 (경로, 제목, 카테고리, 요약).
잘못 분류된 항목을 찾아 JSON 배열로 반환하세요.

카테고리 목록: %s

## 페이지 목록
%s

## 규칙
- 제목/요약을 보고 카테고리가 명백히 틀린 것만 지적
- 애매한 경우는 무시 (현재 분류 유지)
- 예: 호수/산/건물 이름이 "인물"로 분류됨 → 지적
- 예: 사람 이름이 "시스템"으로 분류됨 → 지적
- confidence: 분류 오류 확신도 — high(누가 봐도 명백)/medium/low. **high만 자동 수정되니, 정말 확실할 때만 high**를 쓰고 조금이라도 애매하면 medium 이하로.
- 문제 없으면 빈 배열 [] 반환

JSON 배열만 반환. 다른 텍스트 없이.
형식: [{"path":"...", "currentCategory":"...", "correctCategory":"...", "confidence":"high|medium|low", "reason":"..."}]`,
		strings.Join(Categories, ", "), strings.Join(lines, "\n"))

	systemJSON, _ := json.Marshal("You are a wiki category validator. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: 2048,
	})
	if err != nil {
		wd.logger.Warn("wiki-verify: LLM misclassification check failed", "error", err)
		return nil
	}

	results, err := jsonutil.UnmarshalLLMArray[misclassificationResult](resp)
	if err != nil {
		wd.logger.Warn("wiki-verify: failed to parse LLM response",
			"error", err, "raw", truncate(strings.TrimSpace(resp), 200))
		return nil
	}

	var findings []VerifyFinding
	for _, r := range results {
		f := VerifyFinding{
			Type:   "misclassified",
			Detail: fmt.Sprintf("%s → %s (%s)", r.CurrentCategory, r.CorrectCategory, r.Reason),
			PageA:  r.Path,
		}
		// Attach an auto-applicable move ONLY when the LLM is highly confident
		// and the target is a real, different category — a low-confidence guess
		// stays advisory and never moves a real page.
		if strings.EqualFold(strings.TrimSpace(r.Confidence), "high") {
			if np := recategorizedPath(r.Path, r.CorrectCategory); np != "" {
				f.Fix = &VerifyFix{Kind: "move", NewPath: np}
			}
		}
		findings = append(findings, f)
	}

	return findings
}

// levenshtein computes the edit distance between two strings (rune-level).
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la, lb := len(ra), len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev = curr
	}
	return prev[lb]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
