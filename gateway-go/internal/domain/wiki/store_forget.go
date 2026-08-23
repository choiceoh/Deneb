// store_forget.go — hard "forget this fact" deletion with an audit tombstone.
//
// This is the destructive counterpart to MarkSuperseded (store_merge.go): where
// supersession keeps a stale page readable and merely demotes it in search,
// Forget REMOVES the page from active memory. The trade is deliberate — a
// privacy/correctness "forget this" that left the fact readable would not be a
// forget. The page itself is gone; what remains is the audit log — a "forget"
// tombstone (path, title, reason) alongside the delete path's standard entry —
// so the removal stays accountable.
package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
)

// reservedForgetFiles are wiki-root bookkeeping files that must never be
// hard-deleted through forget: log.md is the audit log (it holds the tombstone
// forget itself writes), index.md is the master search index. Deleting either
// corrupts the store rather than forgetting a fact.
var reservedForgetFiles = map[string]struct{}{
	"log.md":   {},
	"index.md": {},
}

// ForgetResult reports what a Forget removed.
type ForgetResult struct {
	Path  string // normalized page path that was removed
	Title string // the page's title at removal time (for the caller's echo)
}

// Forget removes a page from active memory for privacy or correctness
// ("forget this fact"), first recording an auditable tombstone (page path,
// title, and the caller's reason) to the wiki log. Unlike MarkSuperseded — the
// soft path, which keeps the page readable and only demotes it in search —
// Forget is a HARD delete: the page is gone, and what remains is the audit log
// (the "forget" tombstone with the reason, alongside the delete path's own
// best-effort "delete" entry).
//
// A reason is required and the tombstone is written BEFORE the delete: an
// unaudited forget is not auditable, and this is the one wiki tool path that
// destroys a fact outright, so it fails closed if the audit record can't be
// persisted.
func (s *Store) Forget(relPath, reason string) (ForgetResult, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return ForgetResult{}, fmt.Errorf("wiki: forget needs a page path")
	}
	// Canonicalize BEFORE using the value as an index key. An in-root ".." path
	// (e.g. "기타/../기타/x") deletes the real file via filepath.Join but would
	// prune FTS/master-index/backlinks/semantic under the unclean key, leaving the
	// page searchable. filepath.Clean collapses it to the indexed key.
	relPath = normalizePagePath(filepath.Clean(relPath))
	if err := rejectFactProjectionMutation("forget", relPath); err != nil {
		return ForgetResult{}, err
	}
	// Refuse the store's own bookkeeping files — forgetting them corrupts the
	// wiki (log.md holds the tombstone we are about to write).
	if _, reserved := reservedForgetFiles[relPath]; reserved {
		return ForgetResult{}, fmt.Errorf("wiki: forget: %q is an internal wiki file, not a page", relPath)
	}
	// Refuse deal ledger pages: they mirror the financial deal records, a
	// business/audit surface that a privacy forget must not silently erase.
	if IsDealLedgerPath(relPath) {
		return ForgetResult{}, fmt.Errorf("wiki: forget: %q is a 거래 원장 페이지(재무 감사 기록)라 forget 대상이 아닙니다", relPath)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ForgetResult{}, fmt.Errorf("wiki: forget needs a reason (audit trail)")
	}

	// Hold writeMu across read → audit → delete so no concurrent writer slips an
	// edit into the page between capturing its identity and removing it.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	page, err := s.ReadPage(relPath)
	if err != nil {
		return ForgetResult{}, fmt.Errorf("wiki: forget: read %q: %w", relPath, err)
	}
	title := page.Meta.Title

	// Tombstone first: fail closed if we can't record why the fact was removed.
	// Flatten title/reason to a single line so a multi-line value can't inject a
	// fake "## [ts] op" heading into log.md and corrupt the audit structure.
	logTitle := strings.Join(strings.Fields(title), " ")
	logReason := strings.Join(strings.Fields(reason), " ")
	if err := s.appendLog("forget", fmt.Sprintf("%s — %s — reason: %s", relPath, logTitle, logReason)); err != nil {
		return ForgetResult{}, fmt.Errorf("wiki: forget: record tombstone: %w", err)
	}

	// deletePageLocked (not DeletePage) because we already hold writeMu; it also
	// cleans the FTS index, master index, and backlinks.
	if err := s.deletePageLocked(relPath); err != nil {
		return ForgetResult{}, fmt.Errorf("wiki: forget: delete: %w", err)
	}

	// Drop the semantic vector synchronously. deletePageLocked only clears the
	// lexical (FTS) index; the embedding would otherwise linger until the next
	// async refresh, and semantic recall ranks live vectors — a privacy "forget"
	// must not leave the page returnable in that window.
	s.dropSemanticVector(relPath)

	// Strip lingering inbound Related refs. deletePageLocked removes backlinks
	// derived from the forgotten page's OWN Related, but a one-way reference
	// (legacy/manual drift) on another page keeps the forgotten path in its
	// frontmatter — and Related is indexed, so the path could still surface.
	s.pruneInboundReferences(relPath)

	return ForgetResult{Path: relPath, Title: title}, nil
}

// pruneInboundReferences removes relPath from the Related list of every page
// that still references it. The caller must hold writeMu (writePageInternal
// assumes it). Best-effort: an unreadable page is skipped.
func (s *Store) pruneInboundReferences(relPath string) {
	norm := normalizePagePath(relPath)
	for _, p := range s.findPagesReferencingPath(relPath) {
		page, err := s.ReadPage(p)
		if err != nil {
			continue
		}
		kept := make([]string, 0, len(page.Meta.Related))
		changed := false
		for _, r := range page.Meta.Related {
			if normalizePagePath(r) == norm {
				changed = true
				continue
			}
			kept = append(kept, r)
		}
		if !changed {
			continue
		}
		page.Meta.Related = kept
		_ = s.writePageInternal(p, page, true) // best-effort; backlinks already handled
	}
}
