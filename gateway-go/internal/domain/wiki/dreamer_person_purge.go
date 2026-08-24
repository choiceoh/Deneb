// dreamer_person_purge.go — delete 인물 stub pages that never grew content.
//
// Mention-driven seeding (person_seed.go) creates address-book stubs so later
// cycles can enrich them, but most never are: on 2026-08-24 the 인물 category
// held 292 pages of which 261 carried no prose at all. Search already demotes
// stubs (W8) and related enrichment skips them (the namesake-edge guard in
// verify.go), so nothing ever lifts them out — they exist only as graph noise,
// dreamer scan overhead, and ASR-hotword dilution.
//
// Operator's standing order (2026-08-24): a person page with no content does
// not deserve to exist. Deleting loses nothing — the contact detail lives in
// the contacts mirror, and a person who matters again is re-seeded by the next
// dream cycle that sees them mentioned repeatedly. Purging via DeletePage also
// removes the page from every surviving Related list, which is what finally
// clears the legacy namesake edges the enrichment guard could only stop
// growing.
package wiki

import (
	"path/filepath"
	"time"
)

const (
	// personStubPurgeGraceDays gives a fresh seed a fair chance: enrichment
	// happens on later cycles, so a stub is only "never grew content" once
	// several cycles have passed it over.
	personStubPurgeGraceDays = 14
	// personStubPurgeMaxPerCycle bounds the native-sync/audit-log burst of one
	// cycle. The 2026-08 backlog (261 pages) drains in a handful of cycles.
	personStubPurgeMaxPerCycle = 50
)

// purgeDreamPersonStubs deletes 인물 pages that are still template-only stubs
// past the grace window. Returns how many pages were deleted.
func (wd *WikiDreamer) purgeDreamPersonStubs(now time.Time) int {
	if wd.store == nil {
		return 0
	}
	relPaths, err := wd.store.ListPages("인물")
	if err != nil {
		return 0
	}
	cutoff := now.AddDate(0, 0, -personStubPurgeGraceDays).Format("2006-01-02")
	purged := 0
	var examples []string
	for _, rp := range relPaths {
		if purged >= personStubPurgeMaxPerCycle {
			break
		}
		rp = filepath.ToSlash(rp)
		page, rerr := wd.store.ReadPage(rp)
		if rerr != nil || page == nil {
			continue
		}
		if !isPersonStubPage(rp, page) {
			continue
		}
		// Grace runs from CREATION, not last touch: metadata sweeps (contact
		// re-sync, backlink maintenance) refresh Updated across the whole
		// category — 63 of the 261 backlog stubs were "updated" within a day
		// of the audit — so an Updated-based grace would defer the purge
		// forever. What the window protects is the enrichment opportunity
		// after seeding, and that clock starts at Created. A page with no
		// parseable date predates the date convention — old by definition.
		if born := seededDate(page); born != "" && born > cutoff {
			continue
		}
		if derr := wd.store.DeletePage(rp); derr != nil {
			wd.logger.Warn("wiki-dream: person stub purge failed", "path", rp, "error", derr)
			continue
		}
		purged++
		if len(examples) < 5 {
			examples = append(examples, rp)
		}
	}
	if purged > 0 {
		wd.logger.Info("wiki-dream: purged contentless person stubs",
			"purged", purged, "examples", examples)
	}
	return purged
}

// seededDate is when the page came into being: Created, falling back to
// Updated only when Created was never stamped (YYYY-MM-DD strings compare
// lexicographically), or "" when neither is set.
func seededDate(page *Page) string {
	if page.Meta.Created != "" {
		return page.Meta.Created
	}
	return page.Meta.Updated
}
