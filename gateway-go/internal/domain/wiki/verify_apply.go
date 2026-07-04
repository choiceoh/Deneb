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
)

// maxAutoVerifyFixes caps how many auto-corrections one dream cycle applies, so
// a bad LLM pass (or a sudden pile of dups) can't churn the whole wiki at once.
// Raised 5→15 with the 2026-07 layout cleanup: exact/normalized duplicate merges
// and stale-supersede archivals are cheap, reversible (git snapshot), and a
// backlog that outpaces the cap is precisely how the duplicate pile formed.
const maxAutoVerifyFixes = 15

// applyVerifyFixes auto-applies the high-confidence findings (those carrying a
// Fix) and returns the count applied. Findings without a Fix are ignored here —
// they remain in the report as advisory items.
func (wd *WikiDreamer) applyVerifyFixes(findings []VerifyFinding) int {
	applied := 0
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
			if err := wd.store.MovePage(f.PageA, f.Fix.NewPath); err != nil {
				wd.logger.Warn("wiki-verify: auto-move failed",
					"from", f.PageA, "to", f.Fix.NewPath, "error", err)
				continue
			}
			wd.logger.Info("wiki-verify: auto-moved misclassified page",
				"from", f.PageA, "to", f.Fix.NewPath)
			applied++
		case "merge":
			if err := wd.store.FoldDuplicate(f.PageA, f.PageB); err != nil {
				wd.logger.Warn("wiki-verify: auto-merge failed",
					"keep", f.PageA, "fold", f.PageB, "error", err)
				continue
			}
			wd.logger.Info("wiki-verify: auto-merged duplicate",
				"keep", f.PageA, "fold", f.PageB)
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
	if keep == fold {
		return fmt.Errorf("wiki: cannot fold a page into itself")
	}
	// Cross-project rep-pair guard: FoldDuplicate is the AUTOMATIC merge
	// primitive (dream verify, wiki reviewer), and folding one project's
	// 대표페이지 into another project's orphans the folded folder's children
	// (로그/메일분석/기자재 stay behind while the project vanishes from
	// knownProjects). Same-folder folds (flat remnant → 대표.md slot) pass —
	// that's a layout repair. Operator-driven project merges go through
	// MergePage (the miniapp merge), which is deliberately not guarded.
	if crossProjectRepPair(keep, fold) {
		return fmt.Errorf("wiki: refusing to auto-fold 대표페이지 of different projects (%s ← %s): children need re-parenting first", keep, fold)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.mergePageLocked(keep, fold, func(keepPage, foldPage *Page) string {
		return strings.TrimRight(keepPage.Body, "\n") +
			"\n\n## 병합된 중복 문서 (" + fold + ")\n\n" + foldPage.Body
	})
	return err
}

// archivePage sets a page's Archived flag in place (no move, no delete) — the
// soft retirement used for long-superseded pages and mail-retention archiving.
// Archived pages drop out of Tier-1 injection and research selection and are
// heavily demoted in search.
//
// The prior Updated date is deliberately preserved (link_prune's no-stamp
// doctrine: metadata-only repair must not make a page look active). Stamping
// today here made a 90-day-old mail analysis look fresh the moment retention
// archived it — pushing the owning project's dormancy detection out by up to
// another retention window.
func (s *Store) archivePage(relPath string) error {
	return s.UpdatePage(relPath, func(cur *Page) (*Page, error) {
		if cur == nil {
			return nil, fmt.Errorf("archive %q: not found", relPath)
		}
		if cur.Meta.Archived {
			return nil, nil // already archived — no-op
		}
		cur.Meta.Archived = true
		return cur, nil
	})
}

// exactDupFinding builds a high-confidence duplicate finding, keeping the
// higher-importance page (a later Updated date breaks ties) and folding the
// other into it. entries is an index snapshot (Store.SnapshotEntries).
//
// Exception: two 대표페이지 of DIFFERENT project folders stay advisory (no Fix).
// Normalization-identical project names ("기아 화성" / "기아-화성") produce rep
// pairs whose auto-fold would leave the folded project's children (로그.md,
// 메일분석/*, 기자재/*) orphaned — the project vanishes from knownProjects /
// 모아보기 / 리서치 / 회전 / 휴면 while its files remain. Folding rep pairs safely
// requires re-parenting the children, which is beyond an auto-fix; the
// operator merges via the native client when it's really one project.
func exactDupFinding(entries map[string]IndexEntry, pathA, pathB, detail string) VerifyFinding {
	if crossProjectRepPair(pathA, pathB) {
		return VerifyFinding{
			Type:   "duplicate",
			Detail: detail + " — 서로 다른 프로젝트 폴더의 대표페이지: 하위 문서 재부모화가 필요해 자동 병합하지 않음",
			PageA:  pathA,
			PageB:  pathB,
		}
	}
	keep, fold := pathA, pathB
	if dupKeepSecond(entries, pathA, pathB) {
		keep, fold = pathB, pathA
	}
	return VerifyFinding{
		Type:   "duplicate",
		Detail: detail,
		PageA:  keep,
		PageB:  fold,
		Fix:    &VerifyFix{Kind: "merge"},
	}
}

// crossProjectRepPair reports whether a and b are 대표페이지 of two DIFFERENT
// project folders (legacy flat and in-folder forms both count as reps). A
// same-folder pair — the flat remnant plus its 대표.md slot — is NOT cross-
// project and stays auto-foldable (that fold is a layout repair).
func crossProjectRepPair(a, b string) bool {
	if !IsProjectRepPage(a) || !IsProjectRepPage(b) {
		return false
	}
	fa, oka := ProjectFolderOf(a)
	fb, okb := ProjectFolderOf(b)
	return oka && okb && fa != fb
}

// dupKeepSecond reports whether pathB should be the keeper — true when pathB
// has higher importance, or equal importance but a later Updated date. An
// archived page never wins over a live one, regardless of importance/Updated:
// keeping the archived side would fold the live page INTO retirement and
// vanish it from the active surface (search demotion, Tier-1/research drop).
func dupKeepSecond(entries map[string]IndexEntry, pathA, pathB string) bool {
	a, b := entries[pathA], entries[pathB]
	if a.Archived != b.Archived {
		return a.Archived // exactly one archived → keep the live one
	}
	if b.Importance != a.Importance {
		return b.Importance > a.Importance
	}
	return b.Updated > a.Updated
}

// recategorizedPath swaps a page path's leading category directory for newCat,
// returning "" when newCat isn't a valid taxonomy category, equals the current
// one, or the path has no category segment to replace. The guard is what keeps a
// bogus LLM "correctCategory" from producing a junk move target.
//
// Project-owned paths never get a move target: swapping the leading directory
// would relocate a project SLOT page (대표.md/로그.md/메일분석/*) out of 프로젝트/,
// severing it from its folder and breaking the whole project (knownProjects,
// 모아보기, 회전, 휴면 all key off the folder). Those findings stay advisory.
func recategorizedPath(path, newCat string) string {
	newCat = strings.TrimSpace(newCat)
	if !ValidateCategory(newCat) {
		return ""
	}
	if _, ok := ProjectNameOf(path); ok {
		return ""
	}
	cur, rest, ok := strings.Cut(path, "/")
	if !ok || cur == newCat {
		return ""
	}
	return newCat + "/" + rest
}
