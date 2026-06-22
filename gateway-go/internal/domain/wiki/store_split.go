// store_split.go — oversized-page splitting: SplitPage rewrites a page as a
// table of contents and moves its H2 sections into child pages. Split from
// store.go (Store core).
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SplitPage splits an oversized page into sub-pages by H2 sections.
// The parent page is rewritten as a table-of-contents linking to sub-pages.
// Sub-pages inherit the parent's metadata.
// Returns the paths of created sub-pages, or nil if splitting was not needed.
func (s *Store) SplitPage(relPath string, maxBytes int) ([]string, error) {
	// Hold writeMu across the read + the sub-page/parent rewrites so the split
	// (which reads the oversized body, then overwrites the parent as a TOC) can't
	// race a concurrent writer of the same page. Sub-page and parent writes below
	// go through writePageLocked, not WritePage, because we already hold the lock.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	page, err := s.ReadPage(relPath)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}

	// Check actual size.
	abs := filepath.Join(s.dir, relPath)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat page: %w", err)
	}
	if info.Size() <= int64(maxBytes) {
		return nil, nil
	}

	preamble, sections := page.SplitByH2()
	if len(sections) < 2 {
		// Cannot split meaningfully — single section or no H2 headings.
		return nil, nil
	}

	// Build sub-pages by grouping consecutive sections under the byte budget.
	parentDir := filepath.Dir(relPath)
	parentBase := strings.TrimSuffix(filepath.Base(relPath), ".md")

	type subPage struct {
		path     string
		sections []H2Section
	}

	var subs []subPage
	var currentSections []H2Section
	var currentSize int

	// Reserve budget for frontmatter overhead (~300 bytes).
	budget := maxBytes - 300

	for _, sec := range sections {
		secSize := len(sec.Heading) + len(sec.Content) + 10 // "## heading\n\ncontent\n"
		if len(currentSections) > 0 && currentSize+secSize > budget {
			slug := sectionSlug(currentSections[0].Heading)
			subPath := filepath.Join(parentDir, parentBase+"--"+slug+".md")
			subs = append(subs, subPage{path: subPath, sections: currentSections})
			currentSections = nil
			currentSize = 0
		}
		currentSections = append(currentSections, sec)
		currentSize += secSize
	}
	if len(currentSections) > 0 {
		slug := sectionSlug(currentSections[0].Heading)
		subPath := filepath.Join(parentDir, parentBase+"--"+slug+".md")
		subs = append(subs, subPage{path: subPath, sections: currentSections})
	}

	// If everything fits in one sub-page, splitting is pointless.
	if len(subs) <= 1 {
		return nil, nil
	}

	// Create sub-pages.
	var createdPaths []string
	var tocLines []string
	today := time.Now().Format("2006-01-02")

	for _, sp := range subs {
		sub := NewPage(page.Meta.Title+" — "+sp.sections[0].Heading, page.Meta.Category, page.Meta.Tags)
		sub.Meta.Importance = page.Meta.Importance
		sub.Meta.Related = []string{relPath}

		var body strings.Builder
		for _, sec := range sp.sections {
			body.WriteString("## " + sec.Heading + "\n\n")
			if sec.Content != "" {
				body.WriteString(sec.Content + "\n\n")
			}
		}
		sub.Body = strings.TrimSpace(body.String())

		if err := s.writePageLocked(sp.path, sub); err != nil {
			return createdPaths, fmt.Errorf("write sub-page %s: %w", sp.path, err)
		}
		createdPaths = append(createdPaths, sp.path)

		label := sp.sections[0].Heading
		tocLines = append(tocLines, fmt.Sprintf("- [[%s]] — %s", sp.path, label))
	}

	// Rewrite parent page as a TOC.
	page.Meta.Updated = today
	var parentBody strings.Builder
	if preamble != "" {
		parentBody.WriteString(preamble + "\n\n")
	}
	parentBody.WriteString("## 하위 문서\n\n")
	parentBody.WriteString(strings.Join(tocLines, "\n"))
	parentBody.WriteByte('\n')
	page.Body = parentBody.String()
	page.Meta.Related = append(page.Meta.Related, createdPaths...)

	if err := s.writePageLocked(relPath, page); err != nil {
		return createdPaths, fmt.Errorf("rewrite parent: %w", err)
	}

	return createdPaths, nil
}

// sectionSlug converts a section heading to a short file-safe slug.
func sectionSlug(heading string) string {
	heading = strings.ToLower(heading)
	var sb strings.Builder
	for _, r := range heading {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllable
			sb.WriteRune(r)
		case r >= 0x3131 && r <= 0x318E: // Hangul jamo
			sb.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			sb.WriteByte('-')
		}
	}
	slug := strings.TrimRight(sb.String(), "-")
	if len([]rune(slug)) > 30 {
		slug = string([]rune(slug)[:30])
	}
	if slug == "" {
		slug = "section"
	}
	return slug
}

// pruneGhostEntries removes index entries whose files no longer exist on disk.
