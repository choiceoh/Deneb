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
			wd.logger.Info("wiki-verify: archived long-superseded page", "path", f.PageA)
			applied++
		}
	}
	return applied
}

// FoldDuplicate folds the `fold` page into `keep`: the folded body is appended
// under a "병합된 중복 문서" marker (so nothing is lost), related/tags are unioned,
// and the folded page is deleted. Crude but safe — no LLM synthesis — which is
// the right tradeoff for an automatic duplicate merge. Shared by the dream
// cycle's verify pass and the background wiki reviewer.
func (s *Store) FoldDuplicate(keep, fold string) error {
	foldPage, err := s.ReadPage(fold)
	if err != nil || foldPage == nil {
		return fmt.Errorf("read fold %q: %w", fold, err)
	}
	// Apply the fold onto keep via UpdatePage so a concurrent writer of keep can't
	// be clobbered by this append. fold (read above) is about to be deleted, so a
	// stale read of it is harmless.
	if err := s.UpdatePage(keep, func(keepPage *Page) (*Page, error) {
		if keepPage == nil {
			return nil, fmt.Errorf("read keep %q: not found", keep)
		}
		keepPage.Body = strings.TrimRight(keepPage.Body, "\n") +
			"\n\n## 병합된 중복 문서 (" + fold + ")\n\n" + foldPage.Body
		keepPage.Meta.Related = mergeRelated(keepPage.Meta.Related, foldPage.Meta.Related)
		keepPage.Meta.Tags = mergeTags(keepPage.Meta.Tags, foldPage.Meta.Tags)
		keepPage.Meta.Updated = time.Now().Format("2006-01-02")
		return keepPage, nil
	}); err != nil {
		return fmt.Errorf("write merged %q: %w", keep, err)
	}
	if err := s.DeletePage(fold); err != nil {
		return fmt.Errorf("delete folded %q: %w", fold, err)
	}
	return nil
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
// folding the other into it.
func exactDupFinding(idx *Index, pathA, pathB, detail string) VerifyFinding {
	keep, fold := pathA, pathB
	if dupKeepSecond(idx, pathA, pathB) {
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

// dupKeepSecond reports whether pathB should be the keeper — true when pathB has
// higher importance, or equal importance but a later Updated date.
func dupKeepSecond(idx *Index, pathA, pathB string) bool {
	a, b := idx.Entries[pathA], idx.Entries[pathB]
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
	return newCat + "/" + rest
}
