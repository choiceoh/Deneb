// verify.go — Wiki verification: duplicate entity detection + misclassification check.
// Called as Phase 5 of the WikiDreamer cycle. Detection only — no auto-fix.
package wiki

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// verifyFinding represents a single verification issue found.
type verifyFinding struct {
	Type   string `json:"type"`            // "duplicate", "misclassified", or "stale_deadline"
	Detail string `json:"detail"`          // human-readable description (Korean)
	PageA  string `json:"pageA"`           // primary page path
	PageB  string `json:"pageB,omitempty"` // secondary page path (for duplicates)
	// Fix is set only on HIGH-CONFIDENCE findings the dream cycle may auto-apply
	// (Phase 5): an exact-duplicate merge or an LLM-high-confidence category move.
	// Nil means advisory-only — surfaced in the report, never auto-touched.
	Fix *verifyFix `json:"fix,omitempty"`
}

// wikiVerifyMisclassMaxTokens budgets the category-verdict JSON array. 2048
// was eaten by residual reasoning on dual-mode models (empty content,
// finish_reason=length) even with thinking disabled.
const wikiVerifyMisclassMaxTokens = 8192

// verifyFix is the structured, auto-applicable correction attached to a
// high-confidence verifyFinding. Conservative by construction: only the two
// safe, reversible (git-recoverable) actions are expressible.
type verifyFix struct {
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
func (wd *WikiDreamer) verifyPages(ctx context.Context) []verifyFinding {
	// Snapshot once: the detectors below walk the entries (and the LLM pass
	// holds them across a network call) — iterating the live index map would
	// race concurrent page writers.
	entries := wd.store.snapshotEntries()
	if len(entries) < 2 {
		return nil
	}

	var findings []verifyFinding

	// 5a: Duplicate detection (pure computation).
	findings = append(findings, detectDuplicates(entries)...)

	// 5b: Misclassification detection (single LLM call).
	if wd.client != nil {
		findings = append(findings, wd.detectMisclassifications(ctx, entries)...)
	}

	// 5c: Stale-deadline detection (pure computation).
	findings = append(findings, wd.detectStaleDeadlines()...)

	// 5c-2: Rep-title rule violations (pure computation, advisory — a title
	// edit changes search ranking, so the rename stays a human decision).
	findings = append(findings, detectTitleRuleViolations(entries)...)

	// 5d: Long-superseded pages get archived (pure computation). Supersession is
	// a soft flag — without this, superseded zombies pile up in search/index
	// forever (they were a third of the 2026-07 duplicate mess's long tail).
	findings = append(findings, wd.detectStaleSuperseded()...)

	// 5e: Mail-analysis retention (pure computation). One page per mail means
	// the 메일분석 buckets grow forever; past the retention window they are
	// archive material, not working memory.
	findings = append(findings, wd.detectStaleMailAnalyses()...)

	// 5e-2: Address-book / mention stubs that never earned a recall hit.
	findings = append(findings, wd.detectStalePersonStubs()...)

	// 5f: Unrecalled-cold detection (효용 접지). Old low-importance pages that
	// never surfaced in the recall-utility ledger are candidate dead weight the
	// dreamer created and nobody ever used. Advisory only — no auto-fix — because
	// "not yet queried" is a weaker signal than supersession or a passed
	// deadline; the operator decides.
	findings = append(findings, wd.detectUnrecalled()...)

	return findings
}

// unrecalledImportanceCeil caps the importance of an archive-candidate: a page
// the dreamer marked important stays regardless of recall (it may simply not
// have been queried yet). unrecalledFindingLimit caps how many cold pages one
// cycle surfaces, so a large cold tail cannot drown the report.
const (
	unrecalledImportanceCeil = 0.5
	unrecalledFindingLimit   = 8
)

// detectUnrecalled flags pages that are (1) older than the cold threshold, (2)
// low-importance, (3) live (not archived/superseded), (4) outside the policy
// categories that recall does not drive, and (5) absent from the retained
// recall-utility ledger. These are the dreamer's writes that never earned their
// keep. Pure computation over the ledger + page frontmatter; advisory only.
func (wd *WikiDreamer) detectUnrecalled() []verifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	// Retained ledger = the full compaction window: a page absent here is cold
	// over the whole horizon, not merely quiet in the last 30 days.
	recalls := wd.store.recallHitCounts(time.Now().Add(-recallHitRetention))
	cutoff := time.Now().AddDate(0, 0, -unrecalledColdMinDays).Format("2006-01-02")
	var findings []verifyFinding
	for _, rp := range relPaths {
		if len(findings) >= unrecalledFindingLimit {
			break
		}
		rp = filepath.ToSlash(rp) // ListPages walks with the OS separator
		// Category gate: 사용자/시스템 are policy/config, 인물 is a directory
		// consulted by name resolution — absence of a recall hit there does not
		// mean dead. 메일분석 has its own retention detector (5e).
		switch categoryFromPath(rp) {
		case "사용자", "시스템", "인물":
			continue
		}
		if recalls[rp] > 0 {
			continue
		}
		page, err := wd.store.ReadPage(rp)
		if err != nil || page == nil || page.Meta.Archived || page.Meta.SupersededBy != "" {
			continue
		}
		if page.Meta.Importance >= unrecalledImportanceCeil {
			continue
		}
		created := strings.TrimSpace(page.Meta.Created)
		if created == "" || created >= cutoff { // ISO dates compare lexicographically
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		findings = append(findings, verifyFinding{
			Type:   "unrecalled",
			Detail: fmt.Sprintf("장기 미회상 저중요 페이지 %q (생성 %s, 회상 0) — 아카이브 검토", title, created),
			PageA:  rp,
		})
	}
	return findings
}

// categoryFromPath returns a wiki path's category — its first path segment.
func categoryFromPath(rp string) string {
	if i := strings.IndexByte(rp, '/'); i > 0 {
		return rp[:i]
	}
	return ""
}

// enrichRelatedLinks adds semantic `related` links to pages that currently have
// none, via Store.suggestRelated (high cosine floor). Conservative by design:
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
		sugg := wd.store.suggestRelated(ctx, rp, maxEnrichPerPage)
		if len(sugg) == 0 {
			continue
		}
		// Apply via UpdatePage so a concurrent writer of rp can't be clobbered by
		// this Related-only edit. suggestRelated (an embedding query) ran above,
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
func (wd *WikiDreamer) detectStaleDeadlines() []verifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	today := time.Now()
	var findings []verifyFinding
	for _, rp := range relPaths {
		page, err := wd.store.ReadPage(rp)
		if err != nil || page == nil || page.Meta.Archived {
			continue // 종결/보관된 페이지의 기한은 더 이상 살아있는 마감이 아니다
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
		findings = append(findings, verifyFinding{
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
func (wd *WikiDreamer) detectStaleSuperseded() []verifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -staleSupersededAfterDays).Format("2006-01-02")
	var findings []verifyFinding
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
		findings = append(findings, verifyFinding{
			Type: "stale_superseded",
			Detail: fmt.Sprintf("%s 이후 방치된 superseded 페이지 (→ %s) — 아카이브",
				last, page.Meta.SupersededBy),
			PageA: rp,
			Fix:   &verifyFix{Kind: "archive"},
		})
	}
	return findings
}

// mailAnalysisArchiveAfterDays is the retention window for per-mail analysis
// pages: past it they get archived (kept on disk + git, demoted in search,
// dropped from Tier-1/research) so the raw-mail long tail can't re-pollute
// recall the way the 2026-07 duplicate pile did.
const mailAnalysisArchiveAfterDays = 90

// detectStaleMailAnalyses flags mail-analysis pages older than the retention
// window with an auto-archive fix. Date basis: Updated (set once at creation —
// the mail sink never rewrites these), falling back to Created.
func (wd *WikiDreamer) detectStaleMailAnalyses() []verifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -mailAnalysisArchiveAfterDays).Format("2006-01-02")
	var findings []verifyFinding
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp)
		if !IsMailAnalysisPath(rp) {
			continue
		}
		page, err := wd.store.ReadPage(rp)
		if err != nil || page == nil || page.Meta.Archived {
			continue
		}
		last := strings.TrimSpace(page.Meta.Updated)
		if last == "" {
			last = strings.TrimSpace(page.Meta.Created)
		}
		if last == "" || last >= cutoff { // ISO dates compare lexicographically
			continue
		}
		findings = append(findings, verifyFinding{
			Type:   "stale_mail_analysis",
			Detail: fmt.Sprintf("보존 기한(%d일) 지난 메일분석 (%s) — 아카이브", mailAnalysisArchiveAfterDays, last),
			PageA:  rp,
			Fix:    &verifyFix{Kind: "archive"},
		})
	}
	return findings
}

const personStubArchiveAfterDays = 30

func isAutoPersonStubText(s string) bool {
	return strings.Contains(s, "주소록 기반 자동 생성") ||
		strings.Contains(s, "드림 사이클 반복 언급으로 자동 생성")
}

// detectStalePersonStubs archives address-book / mention stubs that are 30+
// days old and have never been recalled. Curated 인물 pages (no stub marker)
// are left alone.
func (wd *WikiDreamer) detectStalePersonStubs() []verifyFinding {
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return nil
	}
	recalls := wd.store.RecallUsageScoreCounts(time.Now())
	cutoff := time.Now().AddDate(0, 0, -personStubArchiveAfterDays).Format("2006-01-02")
	var findings []verifyFinding
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp)
		if categoryFromPath(rp) != "인물" {
			continue
		}
		page, rerr := wd.store.ReadPage(rp)
		if rerr != nil || page == nil || page.Meta.Archived {
			continue
		}
		if !isAutoPersonStubText(page.Meta.Summary) && !isAutoPersonStubText(page.Body) {
			continue
		}
		created := strings.TrimSpace(page.Meta.Created)
		if created == "" || created >= cutoff {
			continue
		}
		if u := recalls[rp]; u.Injects > 0 || u.Used() {
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		findings = append(findings, verifyFinding{
			Type:   "stale_person_stub",
			Detail: fmt.Sprintf("인물 스텁 %q (생성 %s, 회상 0) — 아카이브", title, created),
			PageA:  rp,
			Fix:    &verifyFix{Kind: "archive"},
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
// entries is an index snapshot (Store.snapshotEntries).
func detectDuplicates(entries map[string]IndexEntry) []verifyFinding {
	pages := make([]pageRef, 0, len(entries))
	for path, entry := range entries {
		pages = append(pages, pageRef{path: path, title: entry.Title, id: entry.ID})
	}
	// Deterministic order. entries is a map, and Go randomizes map iteration, so
	// the same unordered pair used to render with its two members swapped from
	// one cycle to the next: `유사한 제목: "김유영" ~ "김노영"` on one night and
	// `… "김노영" ~ "김유영"` on the next. Both are the SAME finding, but as
	// distinct strings they double the reported count and defeat any text-keyed
	// dedup downstream. Measured over 14 days of dream reports: 6,632 finding
	// lines collapsing to 1,779 unique, with the top pairs appearing ~62 times in
	// EACH direction. Sorting by path fixes the rendering order and makes the
	// whole verify pass reproducible for a given index snapshot.
	sort.Slice(pages, func(i, j int) bool { return pages[i].path < pages[j].path })

	var findings []verifyFinding
	seen := map[string]struct{}{}

	for i := 0; i < len(pages); i++ {
		for j := i + 1; j < len(pages); j++ {
			a, b := pages[i], pages[j]
			key := a.path + "|" + b.path

			if _, ok := seen[key]; ok {
				continue
			}

			// Mail-analysis guard: a 메일분석 page's identity is its Message ID (메일
			// 1통 = 1페이지), never its subject. Distinct Gmail messages routinely
			// share a subject (reply chains, 4분 만의 재발송 정정, vendor
			// notifications), so the subject-normalization merge below — built for
			// curated pages that splinter by name — must not fold two different
			// mails into one page. Skip the pair unless the IDs match (a genuine
			// same-mail duplicate in two buckets, which SHOULD dedup). This was the
			// root of the 2026-06 mail-analysis mis-merge: 14 pages each swallowed a
			// different-ID mail because their titles normalized equal.
			if IsMailAnalysisPath(a.path) || IsMailAnalysisPath(b.path) {
				if mailAnalysisMsgID(a.path) != mailAnalysisMsgID(b.path) {
					continue
				}
			}

			// Sibling deals (kia-001 vs kia-002) share a client and often
			// a similar title; fuzzy match would merge two live folders.
			// Exact/normalized title equality still fires — that is the
			// same project splintered into two spellings (영산고-태양광).
			aName, aOK := ProjectNameOf(a.path)
			bName, bOK := ProjectNameOf(b.path)
			siblingDeals := aOK && bOK && aName != bName

			// Compare titles. Normalized-key equality ("영산고 태양광" vs
			// "영산고-태양광" vs "영산고태양광") is as safe to auto-merge as an exact
			// match — punctuation/spacing variants are exactly how the same topic
			// splinters across agent writes.
			if a.title != "" && b.title != "" {
				if norm := normalizeTitleKey(a.title); norm != "" && norm == normalizeTitleKey(b.title) {
					findings = append(findings, exactDupFinding(entries, a.path, b.path,
						fmt.Sprintf("동일한 제목(정규화): \"%s\" ~ \"%s\"", a.title, b.title)))
					seen[key] = struct{}{}
					continue
				}
				if isSimilar(a.title, b.title) {
					if siblingDeals {
						continue
					}
					findings = append(findings, verifyFinding{
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
				if siblingDeals {
					continue
				}
				dist := levenshtein(a.id, b.id)
				if dist == 0 {
					findings = append(findings, exactDupFinding(entries, a.path, b.path,
						fmt.Sprintf("동일한 ID: \"%s\"", a.id)))
				} else {
					findings = append(findings, verifyFinding{
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
	// Edit distance is calibrated for alphabets, where one character of a short
	// word is plausible spelling variance ("Smith"/"Smyth"). Korean is syllabic:
	// one syllable of a three-syllable name or a two-syllable place name is a
	// THIRD to a HALF of the whole token — a different word, not a typo. Measured
	// over the live wiki (834 active pages): the distance rule produced 106
	// similar-title warnings per dream cycle, 94 of them 인물↔인물 pairs that are
	// plainly different people (강동민~강동화, 김노영~김유영, 김갑수~김덕수) and
	// most of the rest different places (진도군~완도군, 서산시~아산시, 울산~부산),
	// with essentially no true positive in the set — re-reported every cycle
	// because nothing remembers a dismissal. Same-name-different-spelling in CJK
	// is punctuation and spacing, which normalizeTitleKey already folds exactly
	// (and that is the path allowed to auto-merge). So for CJK text, equality
	// after normalization is the only duplicate signal we trust.
	if containsCJK(a) || containsCJK(b) {
		return false
	}
	maxLen := max(utf8.RuneCountInString(a), utf8.RuneCountInString(b))
	if maxLen <= 5 {
		return dist <= 1
	}
	return dist <= 2 && float64(dist)/float64(maxLen) < 0.3
}

// containsCJK reports whether s carries Hangul, CJK ideographs or Kana — the
// scripts whose unit of meaning is a syllable/character rather than a letter.
func containsCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
			r >= 0x1100 && r <= 0x11FF, // Hangul Jamo
			r >= 0x3130 && r <= 0x318F, // Hangul compatibility Jamo
			r >= 0x4E00 && r <= 0x9FFF, // CJK unified ideographs
			r >= 0x3040 && r <= 0x30FF: // Hiragana + Katakana
			return true
		}
	}
	return false
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
// entries is an index snapshot (Store.snapshotEntries).
func (wd *WikiDreamer) detectMisclassifications(ctx context.Context, entries map[string]IndexEntry) []verifyFinding {
	var lines []string
	for path, entry := range entries {
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

	req := wd.llmRequest("You are a wiki category validator. Respond only with a JSON array.", prompt, wikiVerifyMisclassMaxTokens)
	// Strict-JSON one-shot: disable thinking so the output budget goes to the
	// verdict array, not chain-of-thought. When server wiring already attached a
	// chat_template_kwargs off-switch, do not also set Thinking{disabled}: the
	// OpenAI path would add reasoning_effort="low" before ExtraBody is merged,
	// which can keep dual-mode vLLM models reasoning despite the template toggle.
	if !hasTemplateThinkingOff(wd.llmExtraBody) {
		req.Thinking = &llm.ThinkingConfig{Type: "disabled"}
	}
	resp, err := wd.client.Complete(ctx, req)
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

	var findings []verifyFinding
	for _, r := range results {
		f := verifyFinding{
			Type:   "misclassified",
			Detail: fmt.Sprintf("%s → %s (%s)", r.CurrentCategory, r.CorrectCategory, r.Reason),
			PageA:  r.Path,
		}
		// Attach an auto-applicable move ONLY when the LLM is highly confident,
		// the page it names actually exists, its location is a topic decision
		// (not a layout contract), and the target is a real, different category.
		// A low-confidence guess — or any verdict about a layout-managed path —
		// stays advisory and never moves a real page.
		if strings.EqualFold(strings.TrimSpace(r.Confidence), "high") && wd.moveAllowedFor(entries, r.Path) {
			if np := recategorizedPath(r.Path, r.CorrectCategory); np != "" {
				f.Fix = &verifyFix{Kind: "move", NewPath: np}
			}
		}
		findings = append(findings, f)
	}

	return findings
}

// moveAllowedFor decides whether a misclassification verdict about relPath may
// carry an auto-applied move. Three deterministic refusals, each from a way the
// unguarded path damaged the live wiki in 2026-08:
//
//   - the page must exist in the snapshot the prompt was built from. The model
//     returned already-moved paths (프로젝트/거래/김운.md, moved days earlier),
//     which failed with ENOENT every cycle and would have relocated a real page
//     had the name been recycled;
//   - its location must not be layout-managed (IsLayoutManagedPath) — the deal
//     ledger and project slots are placed by code, not by topic;
//   - a `deal` page is a ledger wherever it currently sits, so a topic verdict
//     never relocates one (인물/거래/*, 업무/거래/* are prior damage of exactly
//     this kind and must not be re-shuffled).
//
// Refusals are logged at Debug: they are the normal case for a noisy verdict
// list, and the finding still surfaces to the operator as advisory.
func (wd *WikiDreamer) moveAllowedFor(entries map[string]IndexEntry, relPath string) bool {
	entry, ok := entries[relPath]
	if !ok {
		wd.logger.Warn("wiki-verify: misclassification names an unknown page, move refused", "path", relPath)
		return false
	}
	if IsLayoutManagedPath(relPath) {
		wd.logger.Debug("wiki-verify: move refused (layout-managed path)", "path", relPath)
		return false
	}
	if strings.EqualFold(strings.TrimSpace(entry.Type), "deal") {
		wd.logger.Debug("wiki-verify: move refused (deal ledger page)", "path", relPath)
		return false
	}
	return true
}

func hasTemplateThinkingOff(extra jsonObject) bool {
	raw, ok := extra["chat_template_kwargs"]
	if !ok {
		return false
	}
	switch kwargs := raw.(type) {
	case map[string]any:
		for _, v := range kwargs {
			if b, ok := v.(bool); ok && !b {
				return true
			}
		}
	case map[string]bool:
		for _, b := range kwargs {
			if !b {
				return true
			}
		}
	}
	return false
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
