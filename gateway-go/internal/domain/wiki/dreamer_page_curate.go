// dreamer_page_curate.go — collapse duplicate bullets on 30KB+ pages.
//
// Split (store_split.go) is the size escape hatch; this is the hygiene pass
// for pages that grew by restating the same status line every cycle. A
// collapse that deletes ≥95% of the page is refused (ACE-style wipe). A
// cleanup under 5% is skipped so we do not bump Updated for noise.
package wiki

import (
	"path/filepath"
	"strings"
)

const (
	pageCurateMinBytes      = 30_000
	pageCurateMinReduction  = 0.05
	pageCurateMaxReduction  = 0.95
	pageCurateMaxPagesCycle = 8
)

func (wd *WikiDreamer) curateOversizedDreamPages() int {
	if wd.store == nil {
		return 0
	}
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return 0
	}
	curated := 0
	for _, rp := range relPaths {
		if curated >= pageCurateMaxPagesCycle {
			break
		}
		rp = filepath.ToSlash(rp)
		page, rerr := wd.store.ReadPage(rp)
		if rerr != nil || page == nil || page.Meta.Archived {
			continue
		}
		if len(page.Body) < pageCurateMinBytes {
			continue
		}
		if _, ok := curatePageBody(page.Body); !ok {
			continue
		}
		werr := wd.store.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil || len(cur.Body) < pageCurateMinBytes {
				return nil, nil
			}
			next, apply := curatePageBody(cur.Body)
			if !apply {
				return nil, nil
			}
			cur.Body = next
			return cur, nil
		})
		if werr != nil {
			if wd.logger != nil {
				wd.logger.Warn("wiki-dream: page curation failed", "path", rp, "error", werr)
			}
			continue
		}
		curated++
		if wd.logger != nil {
			wd.logger.Info("wiki-dream: oversized page curated", "path", rp)
		}
	}
	return curated
}

// curatePageBody collapses duplicate bullets. ok is false when the cleanup
// is too small to be worth a write or so large it looks like a wipe.
func curatePageBody(body string) (string, bool) {
	cleaned := collapseDuplicateBullets(body)
	if cleaned == body {
		return body, false
	}
	orig := len(body)
	if orig == 0 {
		return body, false
	}
	reduction := float64(orig-len(cleaned)) / float64(orig)
	if reduction < pageCurateMinReduction || reduction >= pageCurateMaxReduction {
		return body, false
	}
	return cleaned, true
}

func collapseDuplicateBullets(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "## ") {
			seen = map[string]struct{}{}
			kept = append(kept, ln)
			continue
		}
		if isMarkdownBullet(trim) {
			key := normalizeDedupLine(ln)
			if key != "" {
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
			}
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

func isMarkdownBullet(trim string) bool {
	return strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ")
}
