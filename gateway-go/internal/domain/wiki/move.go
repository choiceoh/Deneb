// move.go — Store.MovePage: relocate a wiki page to a new path (and thus a new
// category, since the bucket is the path's leading directory — Stats uses
// filepath.Dir). Composed from Read + Write + Delete so the FTS index, the
// master index, and backlinks all stay consistent exactly the way every other
// write path maintains them.
package wiki

import (
	"fmt"
	"strings"
)

// MovePage relocates a page from one wiki path to another, updating its
// frontmatter category to match the new top-level directory (the on-disk
// bucket). It never overwrites: an existing target is an error (the caller
// should merge instead). A no-op when from and to normalize to the same path.
//
// Single-operator, last-write-wins. The move is Write-then-Delete, so a crash
// between the two leaves a recoverable duplicate (git history) rather than a
// lost page. Inbound [[wikilinks]] that referenced the old path are not
// rewritten here — the dream cycle re-resolves the graph — but the page's own
// backlinks are maintained by the underlying WritePage/DeletePage.
func (s *Store) MovePage(from, to string) error {
	from = normalizePagePath(from)
	to = normalizePagePath(to)
	if from == to {
		return nil
	}
	// Serialize the whole move (read source, collision-check target, write target,
	// delete source) under writeMu so it can't interleave with a concurrent writer
	// of either page. The writes below go through the *Locked helpers because we
	// already hold the lock.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	src, err := s.ReadPage(from)
	if err != nil {
		return fmt.Errorf("wiki: move: read source %q: %w", from, err)
	}
	if src == nil {
		return fmt.Errorf("wiki: move: source %q not found", from)
	}
	if existing, _ := s.ReadPage(to); existing != nil {
		return fmt.Errorf("wiki: move: target %q already exists", to)
	}
	// Keep the frontmatter category in sync with the new bucket so it doesn't
	// disagree with where the file actually lives.
	if cat, _, ok := strings.Cut(to, "/"); ok {
		src.Meta.Category = cat
	}
	if err := s.writePageLocked(to, src); err != nil {
		return fmt.Errorf("wiki: move: write target %q: %w", to, err)
	}
	if err := s.deletePageLocked(from); err != nil {
		return fmt.Errorf("wiki: move: delete source %q after write: %w", from, err)
	}
	return nil
}
