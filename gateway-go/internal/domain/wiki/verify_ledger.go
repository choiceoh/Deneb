// verify_ledger.go — memory for advisory verify findings across dream cycles.
//
// Phase 5 recomputes findings from scratch every cycle, so an advisory finding
// the operator has already seen (a stale deadline they chose to leave, a
// borderline pair they judged distinct) looked identical to a brand-new
// discovery — and got re-announced every cycle. Measured before the CJK fix
// (#4376): 6,632 report lines over 14 days collapsing to 1,779 unique. This
// ledger keys each advisory finding, so the report can say "new" and fold the
// rest into one count line.
//
// Resolution semantics: an entry whose finding did NOT re-appear this cycle is
// dropped — the underlying issue is gone (page merged, archived, deadline
// updated), so a later recurrence rightfully counts as new again.
package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const verifyLedgerFile = ".verify-findings.json"

type verifyLedgerEntry struct {
	FirstSeen string `json:"firstSeen"` // RFC3339
	LastSeen  string `json:"lastSeen"`
	Count     int    `json:"count"`
}

type verifyLedger struct {
	Version int                          `json:"version"`
	Entries map[string]verifyLedgerEntry `json:"entries"`
}

// verifyFindingKey identifies a finding across cycles. Type + the page pair —
// NOT Detail, which embeds volatile rendering (distances, dates) that would
// break continuity for what is logically the same finding.
func verifyFindingKey(f verifyFinding) string {
	return f.Type + "|" + f.PageA + "|" + f.PageB
}

func (wd *WikiDreamer) verifyLedgerPath() string {
	return filepath.Join(wd.store.Dir(), verifyLedgerFile)
}

func (wd *WikiDreamer) loadVerifyLedger() verifyLedger {
	led := verifyLedger{Version: 1, Entries: map[string]verifyLedgerEntry{}}
	data, err := os.ReadFile(wd.verifyLedgerPath())
	if err != nil {
		return led
	}
	if err := json.Unmarshal(data, &led); err != nil && wd.logger != nil {
		wd.logger.Warn("wiki-dream: verify ledger parse failed", "error", err)
	}
	if led.Entries == nil {
		led.Entries = map[string]verifyLedgerEntry{}
	}
	led.Version = 1
	return led
}

func (wd *WikiDreamer) saveVerifyLedger(led verifyLedger) error {
	data, err := json.MarshalIndent(led, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verify ledger: %w", err)
	}
	path := wd.verifyLedgerPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write verify ledger: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace verify ledger: %w", err)
	}
	return nil
}

// reconcileVerifyFindings folds this cycle's ADVISORY findings (Fix == nil)
// into the ledger and splits them into first-time findings (reported verbatim)
// and repeats (folded into a count). Entries absent from this cycle are
// dropped (resolved). Returns the updated ledger, the fresh findings in their
// original order, and the repeat count.
func reconcileVerifyFindings(led verifyLedger, findings []verifyFinding, now time.Time) (verifyLedger, []verifyFinding, int) {
	ts := now.Format(time.RFC3339)
	next := verifyLedger{Version: 1, Entries: make(map[string]verifyLedgerEntry, len(findings))}
	var fresh []verifyFinding
	repeats := 0
	for _, f := range findings {
		if f.Fix != nil {
			continue // auto-applied, never reported — nothing to remember
		}
		key := verifyFindingKey(f)
		if prev, ok := led.Entries[key]; ok {
			repeats++
			next.Entries[key] = verifyLedgerEntry{FirstSeen: prev.FirstSeen, LastSeen: ts, Count: prev.Count + 1}
			continue
		}
		// Two advisory findings can share a key within one cycle (e.g. a title
		// AND an id finding folded by `seen` upstream make this rare, but stay
		// safe): the first occurrence is the reported one.
		if _, dup := next.Entries[key]; dup {
			continue
		}
		fresh = append(fresh, f)
		next.Entries[key] = verifyLedgerEntry{FirstSeen: ts, LastSeen: ts, Count: 1}
	}
	return next, fresh, repeats
}

// splitAnswerableFindings separates the advisory findings the OPERATOR can
// close (AckPages set — the two person-identity scans) from the ones the
// ledger's first-time/repeat fold governs. The fold exists so the dream card
// announces news; an unfixable question that never changes is never news under
// that rule, which is how the same five people ended up asked forever and
// answerable never.
func splitAnswerableFindings(findings []verifyFinding) (rest, answerable []verifyFinding) {
	for _, f := range findings {
		if f.Fix == nil && len(f.AckPages) > 0 {
			answerable = append(answerable, f)
			continue
		}
		rest = append(rest, f)
	}
	return rest, answerable
}
