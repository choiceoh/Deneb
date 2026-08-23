// verify_apply.go — auto-application of HIGH-CONFIDENCE verify findings during a
// dream cycle (Phase 5). Conservative by design: only two safe, reversible
// (git-recoverable via the per-cycle wiki snapshot) corrections are ever applied
// automatically, and only when the detector attached a structured Fix —
//
//	merge: an EXACT-duplicate pair (identical title or ID) is folded together,
//	       keeping the higher-importance page and appending the other's body
//	       under a marker (zero information loss), then deleting the folded page.
//	move:  a page the misclassification LLM flagged with confidence "high" is
//	       moved to the correct category.
//
// Everything else stays advisory — surfaced in the dream report, never touched.
// Applications are capped per cycle to bound blast radius and every action is
// logged so the operator can audit (and reverse) any auto-fix.
package wiki

import (
	"fmt"
	"strings"
	"time"
)

// maxAutoVerifyFixes caps how many auto-corrections one dream cycle applies, so
// a bad LLM pass (or a sudden pile of dups) can't churn the whole wiki at once.
// Raised 5→15 with the 2026-07 layout cleanup: exact/normalized duplicate merges
// and stale-supersede archivals are cheap, reversible (git snapshot), and a
// backlog that outpaces the cap is precisely how the duplicate pile formed.
const maxAutoVerifyFixes = 15

// maxAutoMovesPerCycle caps the misclassification moves separately from the
// shared fix budget. Merges and archivals are content decisions with their own
// guards; a move is a location decision made by an LLM over the whole index,
// and when it goes wrong it goes wrong in bulk — 2026-08-17 spent the entire
// 15-fix budget relocating deal ledgers in one cycle. A small cap keeps any
// future misfire to a handful of reversible pages per cycle.
const maxAutoMovesPerCycle = 3

// applyVerifyFixes auto-applies the high-confidence findings (those carrying a
// Fix) and returns the count applied. Findings without a Fix are ignored here —
// they remain in the report as advisory items. record, when non-nil, is invoked
// for every applied fix (the caller feeds the fact ledger + DreamReport
// counters, 5.6); it must never panic or block.
func (wd *WikiDreamer) applyVerifyFixes(findings []verifyFinding, record func(f verifyFinding)) int {
	applied := 0
	moves := 0
	for _, f := range findings {
		if f.Fix == nil {
			continue
		}
		if applied >= maxAutoVerifyFixes {
			wd.logger.Info("wiki-verify: auto-fix cap reached, deferring the rest to next cycle",
				"cap", maxAutoVerifyFixes)
			break
		}
		switch f.Fix.Kind {
		case "move":
			if moves >= maxAutoMovesPerCycle {
				wd.logger.Info("wiki-verify: auto-move cap reached, deferring the rest to next cycle",
					"cap", maxAutoMovesPerCycle)
				continue
			}
			if err := wd.store.MovePage(f.PageA, f.Fix.NewPath); err != nil {
				wd.logger.Warn("wiki-verify: auto-move failed",
					"from", f.PageA, "to", f.Fix.NewPath, "error", err)
				continue
			}
			moves++
			// The verdict's reason is the only record of WHY a page moved:
			// findings carrying a Fix are excluded from the advisory ledger and
			// the operator notification, so without it a relocation is
			// unauditable after the fact.
			wd.logger.Info("wiki-verify: auto-moved misclassified page",
				"from", f.PageA, "to", f.Fix.NewPath, "reason", f.Detail)
			if record != nil {
				record(f)
			}
			applied++
		case "merge":
			if err := wd.store.FoldDuplicate(f.PageA, f.PageB); err != nil {
				wd.logger.Warn("wiki-verify: auto-merge failed",
					"keep", f.PageA, "fold", f.PageB, "error", err)
				continue
			}
			wd.logger.Info("wiki-verify: auto-merged duplicate",
				"keep", f.PageA, "fold", f.PageB)
			if record != nil {
				record(f)
			}
			applied++
		case "archive":
			if err := wd.store.archivePage(f.PageA); err != nil {
				wd.logger.Warn("wiki-verify: auto-archive failed", "path", f.PageA, "error", err)
				continue
			}
			// Detail distinguishes WHY (long-superseded vs mail retention 90d —
			// both arrive as "archive" fixes; the old fixed message mislabeled
			// retention archives as superseded).
			wd.logger.Info("wiki-verify: archived page", "path", f.PageA, "detail", f.Detail)
			if record != nil {
				record(f)
			}
			applied++
		}
	}
	return applied
}

// FoldDuplicate folds the `fold` page into `keep`: the folded body is appended
// under a "병합된 중복 문서" marker (so nothing is lost), frontmatter is unioned
// (tags, related, cues, code, importance — same policy as MergePage), inbound
// references are repointed keep-ward, and the folded page is deleted. Crude but
// safe — no LLM synthesis — which is the right tradeoff for an automatic
// duplicate merge. Shared by the dream cycle's verify pass and the background
// wiki reviewer.
//
// Delegates to the same single-writeMu helper as MergePage: the old two-step
// implementation (read fold → UpdatePage(keep) → DeletePage(fold)) took the
// write lock three separate times, so a write landing on fold between the read
// and the delete was silently destroyed, nothing repointed inbound references,
// and fold's cues/code were dropped.
func (s *Store) FoldDuplicate(keep, fold string) error {
	keep = strings.TrimSpace(keep)
	fold = strings.TrimSpace(fold)
	if keep == "" || fold == "" {
		return fmt.Errorf("wiki: fold needs both keep and fold paths")
	}
	// Normalized self-fold guard — mirror MergePage: "기타/dup" vs "기타/dup.md"
	// is the same file, and folding a page into itself deletes it.
	keep = normalizePagePath(keep)
	fold = normalizePagePath(fold)
	if err := rejectFactProjectionMutation("fold duplicate", keep, fold); err != nil {
		return err
	}
	if keep == fold {
		return fmt.Errorf("wiki: cannot fold a page into itself")
	}
	// Hard backstop against the 2026-06 mail-analysis mis-merge: two 메일분석 pages
	// with DIFFERENT Message IDs are two different Gmail messages (메일 1통 =
	// 1페이지) and must never become one page, no matter how the caller decided
	// they were duplicates (title-normalization in dream verify, an LLM verdict in
	// wiki-review, or a future path). Same-ID folds (one mail relocated across
	// buckets) still pass. Detectors also filter this upstream; this is the
	// last-line guarantee at the shared merge chokepoint.
	if IsMailAnalysisPath(keep) && IsMailAnalysisPath(fold) &&
		mailAnalysisMsgID(keep) != mailAnalysisMsgID(fold) {
		return fmt.Errorf("wiki: refusing to fold two distinct mail analyses (%s ≠ %s) — 메일 1통 = 1페이지",
			mailAnalysisMsgID(keep), mailAnalysisMsgID(fold))
	}
	// Same class of backstop for project layout slots. A project folder's 대표 /
	// 로그 / 상세 pages are DIFFERENT documents about one project by design, and
	// two 대표 pages under different code folders are two different projects —
	// neither is ever a duplicate, whatever a title- or id-based detector
	// concluded. Both shapes actually fired in 2026-08 (대표←로그 three times,
	// pl2-kia-epc-001/대표 ← pl2-kia-epc-002/대표 once, which is how a project
	// lost its 대표페이지).
	if err := refuseLayoutSlotFold(keep, fold); err != nil {
		return err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.mergePageLocked(keep, fold, func(keepPage, foldPage *Page) string {
		return strings.TrimRight(keepPage.Body, "\n") +
			"\n\n## 병합된 중복 문서 (" + fold + ")\n\n" + foldPage.Body
	})
	return err
}

// refuseLayoutSlotFold rejects folds between pages whose relationship is fixed
// by the project layout rather than by their content: two pages inside one
// project folder (대표/로그/상세 are distinct documents about the same project),
// and two 대표 pages belonging to different project codes (two projects).
//
// Only in-folder pages (프로젝트/<name>/…) are guarded. The legacy flat form
// 프로젝트/<name>.md is a migration remnant whose repair IS a fold into the
// folder's 대표 slot (wiki-review's layout repair), so folding it must stay
// possible.
func refuseLayoutSlotFold(keep, fold string) error {
	keepProject, keepOK := inFolderProjectOf(keep)
	foldProject, foldOK := inFolderProjectOf(fold)
	if !keepOK || !foldOK {
		return nil
	}
	if keepProject == foldProject {
		return fmt.Errorf("wiki: refusing to fold two pages of project %q (%s ← %s) — 대표/로그/상세는 같은 프로젝트의 다른 문서",
			keepProject, keep, fold)
	}
	// Two 대표 pages under DIFFERENT minted codes are two different deals
	// (pl2-kia-epc-001 vs -002), never a spelling variant of one project — the
	// fold that cost pl2-kia-epc-002 its 대표페이지. Folder names that are not
	// codes (탑솔라 vs 탑솔라-중복) are exactly the splintering that normalized-title
	// merges exist to repair, so those still fold.
	if IsProjectRepPage(keep) && IsProjectRepPage(fold) &&
		validProjectCode(keepProject) && validProjectCode(foldProject) {
		return fmt.Errorf("wiki: refusing to fold 대표 pages of different project codes (%s ← %s)", keep, fold)
	}
	return nil
}

// inFolderProjectOf returns the owning project for a page that lives INSIDE a
// project folder (프로젝트/<name>/…), excluding the legacy flat form.
func inFolderProjectOf(relPath string) (string, bool) {
	if len(splitProjectPath(relPath)) < 2 {
		return "", false
	}
	return ProjectNameOf(relPath)
}

// archivePage sets a page's Archived flag in place (no move, no delete) — the
// soft retirement used for long-superseded pages. Archived pages drop out of
// Tier-1 injection and research selection and are heavily demoted in search.
func (s *Store) archivePage(relPath string) error {
	return s.UpdatePage(relPath, func(cur *Page) (*Page, error) {
		if cur == nil {
			return nil, fmt.Errorf("archive %q: not found", relPath)
		}
		if cur.Meta.Archived {
			return nil, nil // already archived — no-op
		}
		cur.Meta.Archived = true
		cur.Meta.Updated = time.Now().Format("2006-01-02")
		return cur, nil
	})
}

// exactDupFinding builds a high-confidence duplicate finding with a merge Fix,
// keeping the higher-importance page (a later Updated date breaks ties) and
// folding the other into it. entries is an index snapshot (Store.snapshotEntries).
func exactDupFinding(entries map[string]IndexEntry, pathA, pathB, detail string) verifyFinding {
	keep, fold := pathA, pathB
	if dupKeepSecond(entries, pathA, pathB) {
		keep, fold = pathB, pathA
	}
	return verifyFinding{
		Type:   "duplicate",
		Detail: detail,
		PageA:  keep,
		PageB:  fold,
		Fix:    &verifyFix{Kind: "merge"},
	}
}

// dupKeepSecond reports whether pathB should be the keeper — true when pathB has
// higher importance, or equal importance but a later Updated date.
func dupKeepSecond(entries map[string]IndexEntry, pathA, pathB string) bool {
	a, b := entries[pathA], entries[pathB]
	if b.Importance != a.Importance {
		return b.Importance > a.Importance
	}
	return b.Updated > a.Updated
}

// recategorizedPath swaps a page path's leading category directory for newCat,
// returning "" when newCat isn't a valid taxonomy category, equals the current
// one, or the path has no category segment to replace. The guard is what keeps a
// bogus LLM "correctCategory" from producing a junk move target.
func recategorizedPath(path, newCat string) string {
	newCat = strings.TrimSpace(newCat)
	if !ValidateCategory(newCat) {
		return ""
	}
	cur, rest, ok := strings.Cut(path, "/")
	if !ok || cur == newCat {
		return ""
	}
	// Category swaps move flat pages only. Preserving the sub-folder used to
	// mint category/sub-folder combinations nothing else writes or reads
	// (기타/거래/, 인물/거래/, 기타/<projectCode>/) — the 2026-08 ledger split.
	// A page that lives in a slot is placed by layout, not by category.
	if strings.Contains(rest, "/") {
		return ""
	}
	// 프로젝트/ is folder-structured (프로젝트/<code>/대표.md): a page cannot be
	// promoted into it by renaming its leading segment.
	if newCat == projectCategoryPrefix {
		return ""
	}
	return newCat + "/" + rest
}
