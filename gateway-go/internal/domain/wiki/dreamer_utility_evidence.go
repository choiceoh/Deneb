// dreamer_utility_evidence.go — what the recall ledger says about page KINDS.
//
// The rules-revision lane (dreamer_selfcompare.go) rewrites the synthesis rules
// from one signal: which cycle won the self-comparison. That judges style, not
// consequence — the LLM is grading its own prose. The recall ledger holds the
// consequence: of everything the dreamer ever wrote, which KINDS of page later
// got pulled into a real turn and which never did (live 2026-07: 834 active
// pages, 212 ever recalled).
//
// Folding that distribution into the revision prompt makes the evolving rules
// answer "write more of what gets used, less of what doesn't" from measured
// traffic rather than from the judge's taste — the 효용 접지 half of RSI P3.
package wiki

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// utilityEvidenceGroups bounds how many page kinds the prompt block lists.
const utilityEvidenceGroups = 8

// utilityGroup is one page-kind bucket: how many exist, how many earned recall.
type utilityGroup struct {
	Kind     string
	Written  int
	Recalled int
}

// pageKind buckets a page path into the kind a synthesis rule can act on:
// category plus the project-folder slot (대표/로그/메일분석/자료/회의록), which is
// the granularity the layout rules actually speak in.
func pageKind(relPath string) string {
	parts := strings.Split(strings.TrimPrefix(relPath, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "기타"
	}
	category := parts[0]
	if category != "프로젝트" || len(parts) < 3 {
		return category
	}
	// 프로젝트/<name>/<slot>.md — the slot is the second-to-last segment for
	// sub-folders (메일분석/자료/회의록) and the file stem for 대표/로그.
	last := parts[len(parts)-1]
	if len(parts) >= 4 {
		return category + "/" + parts[len(parts)-2]
	}
	switch {
	case strings.HasPrefix(last, "대표"):
		return category + "/대표"
	case strings.HasPrefix(last, "로그"):
		return category + "/로그"
	}
	return category + "/기타"
}

// utilityByKind aggregates written-vs-recalled counts per page kind, most
// written first. `recalls` is the score-window ledger usage; a page counts as
// recalled when it was observably USED (read/cite), not merely injected —
// exposure is not use (bridge-evidence adoption). The index snapshot already
// excludes archived pages, so every entry here is live memory.
func utilityByKind(entries map[string]IndexEntry, recalls map[string]RecallUsage) []utilityGroup {
	agg := map[string]*utilityGroup{}
	for path := range entries {
		kind := pageKind(path)
		g, ok := agg[kind]
		if !ok {
			g = &utilityGroup{Kind: kind}
			agg[kind] = g
		}
		g.Written++
		if usage, hit := recalls[path]; hit && usage.Used() {
			g.Recalled++
		}
	}
	out := make([]utilityGroup, 0, len(agg))
	for _, g := range agg {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Written != out[j].Written {
			return out[i].Written > out[j].Written
		}
		return out[i].Kind < out[j].Kind // deterministic ties
	})
	if len(out) > utilityEvidenceGroups {
		out = out[:utilityEvidenceGroups]
	}
	return out
}

// renderUtilityEvidence formats the distribution for the revision prompt.
// Returns "" when there is no ledger traffic yet — an empty block is better
// than teaching the rules from zero evidence.
func (wd *WikiDreamer) renderUtilityEvidence(now time.Time) string {
	recalls := wd.store.RecallUsageScoreCounts(now)
	if len(recalls) == 0 {
		return ""
	}
	groups := utilityByKind(wd.store.snapshotEntries(), recalls)
	if len(groups) == 0 {
		return ""
	}
	var b strings.Builder
	for _, g := range groups {
		pct := 0.0
		if g.Written > 0 {
			pct = float64(g.Recalled) / float64(g.Written) * 100
		}
		fmt.Fprintf(&b, "- %s: 보유 %d쪽, 실제 회상 %d쪽 (%.0f%%)\n", g.Kind, g.Written, g.Recalled, pct)
	}
	// Demand: what the wiki was asked for and could not answer. Written pages
	// with no recall are one failure mode; questions with no page at all are
	// the other, and only this ledger sees them.
	if terms := wd.store.RecallDemandTerms(now, 8); len(terms) > 0 {
		fmt.Fprintf(&b, "\n답하지 못한 질문 주제(수요): %s\n", strings.Join(terms, ", "))
	}
	return b.String()
}

// dreamerWrittenUtilityByKind is the dreamer-scoped counterpart of
// utilityByKind: it buckets only the pages the DREAMER wrote (the capsule
// history) into page kinds and counts written-vs-recalled. utilityByKind spans
// the whole index — operator- and system-authored pages dominate the
// denominator and dilute the signal about the dreamer's own routing. This is
// the honest feedback: of the pages the dreamer chose to create, which kinds
// later earned recall. The grace/window filters mirror computeDreamQuality so
// the numbers the revision prompt sees agree with the numbers the quality score
// reports.
func dreamerWrittenUtilityByKind(capsules []processedDiaryCapsule, recalls map[string]RecallUsage, now time.Time) []utilityGroup {
	agg := map[string]*utilityGroup{}
	seen := map[string]struct{}{}
	for _, cap := range capsules {
		at, err := time.Parse(time.RFC3339, cap.At)
		if err != nil || now.Sub(at) < utilityGrace {
			continue // too fresh to fairly judge
		}
		if now.Sub(at) > utilityWindow {
			continue // past the ledger's judgment window
		}
		for _, p := range cap.Paths {
			if p == "" {
				continue
			}
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			kind := pageKind(p)
			g, ok := agg[kind]
			if !ok {
				g = &utilityGroup{Kind: kind}
				agg[kind] = g
			}
			g.Written++
			if usage, hit := recalls[p]; hit && usage.Used() {
				g.Recalled++
			}
		}
	}
	out := make([]utilityGroup, 0, len(agg))
	for _, g := range agg {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Written != out[j].Written {
			return out[i].Written > out[j].Written
		}
		return out[i].Kind < out[j].Kind
	})
	if len(out) > utilityEvidenceGroups {
		out = out[:utilityEvidenceGroups]
	}
	return out
}

// renderDreamerUtilityEvidence formats the dreamer-scoped written-vs-recalled
// distribution for the revision prompt. Returns "" when there is no recall
// ledger traffic or no judgeable dreamer-written capsules yet — an empty block
// is better than teaching the rules from zero evidence.
func (wd *WikiDreamer) renderDreamerUtilityEvidence(now time.Time) string {
	recalls := wd.store.RecallUsageScoreCounts(now)
	if len(recalls) == 0 {
		return ""
	}
	capsules := wd.loadDiaryProcessState().Recent
	groups := dreamerWrittenUtilityByKind(capsules, recalls, now)
	if len(groups) == 0 {
		return ""
	}
	var b strings.Builder
	for _, g := range groups {
		pct := 0.0
		if g.Written > 0 {
			pct = float64(g.Recalled) / float64(g.Written) * 100
		}
		fmt.Fprintf(&b, "- %s: 드리머 작성 %d쪽, 실제 회상 %d쪽 (%.0f%%)\n", g.Kind, g.Written, g.Recalled, pct)
	}
	return b.String()
}
