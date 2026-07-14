package genesis

// Meta-revision class instrumentation — the "L1.5 trap" telemetry.
//
// Bilevel Autoresearch (arXiv 2603.23420) found that parameter-level tweaks of
// a fixed improvement mechanism yield no reliable gain, while STRUCTURAL
// mechanism change produced the entire improvement (5x in their ablation). A
// meta loop whose adopted revisions are all parametric is quietly rebuilding
// that null result — and a non-regression gate stack systematically favors
// timid edits, so the drift is invisible without telemetry.
//
// Everything here is ADVISORY (informs vs decides): the classifier feeds the
// ledger, the rsi_status L2 metrics, and a streak-triggered evidence nudge for
// the producer epoch. No gate, bench, or drift brake reads it.

import (
	"fmt"
	"regexp"
	"strings"
)

// Meta-revision classes. A revision is structural when the prompt's skeleton
// (headings + numbered rules) changed or the body was substantially rewritten;
// parametric when it tweaks wording/values inside the existing skeleton.
const (
	MetaRevisionClassStructural = "structural"
	MetaRevisionClassParametric = "parametric"
)

// metaStructuralRewriteRatio: a proposal that keeps the skeleton but replaces
// this fraction of body lines is a rewrite of the procedure, not a tweak.
const metaStructuralRewriteRatio = 0.35

// metaClassBalanceWindow bounds the proposed-revision tally in the balance
// aggregate; metaParametricStreakNudge is the consecutive-parametric-adoption
// count that triggers the producer-epoch evidence nudge and the rsi_status
// warning metric.
const (
	metaClassBalanceWindow    = 20
	metaParametricStreakNudge = 3
)

var metaNumberedRuleRe = regexp.MustCompile(`^\d+[.)]\s`)

// promptSkeleton extracts the structural lines of a prompt: markdown headings
// (full text — renaming a section changes what the section IS) and numbered
// rule MARKERS (number only — rewording a rule's text is the archetypal
// parametric tweak, e.g. "quantify rule 14"; adding/removing a rule changes
// the mechanism). Order matters — reordering sections is a structural change.
func promptSkeleton(s string) []string {
	var skel []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "#"):
			skel = append(skel, t)
		case metaNumberedRuleRe.MatchString(t):
			skel = append(skel, strings.TrimSpace(metaNumberedRuleRe.FindString(t)))
		}
	}
	return skel
}

// changedLineRatio is the symmetric multiset difference of non-empty trimmed
// lines over the total line count of both texts — 0.0 for identical bodies,
// approaching 1.0 for a full rewrite.
func changedLineRatio(a, b string) float64 {
	counts := map[string]int{}
	total := 0
	for _, line := range strings.Split(a, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			counts[t]++
			total++
		}
	}
	for _, line := range strings.Split(b, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			counts[t]--
			total++
		}
	}
	if total == 0 {
		return 0
	}
	changed := 0
	for _, c := range counts {
		if c < 0 {
			c = -c
		}
		changed += c
	}
	return float64(changed) / float64(total)
}

// classifyMetaRevision deterministically classifies a proposed revision
// against its incumbent. Pure — no Evolver/Tracker state.
func classifyMetaRevision(incumbent, proposal string) (class, detail string) {
	incSkel := promptSkeleton(incumbent)
	propSkel := promptSkeleton(proposal)
	if len(incSkel) != len(propSkel) {
		return MetaRevisionClassStructural,
			fmt.Sprintf("skeleton lines %d→%d", len(incSkel), len(propSkel))
	}
	for i := range incSkel {
		if incSkel[i] != propSkel[i] {
			return MetaRevisionClassStructural,
				fmt.Sprintf("skeleton line %d changed: %q→%q", i+1,
					truncateMetaDetail(incSkel[i], 40), truncateMetaDetail(propSkel[i], 40))
		}
	}
	ratio := changedLineRatio(incumbent, proposal)
	if ratio >= metaStructuralRewriteRatio {
		return MetaRevisionClassStructural,
			fmt.Sprintf("body rewrite ratio %.2f >= %.2f (skeleton unchanged)", ratio, metaStructuralRewriteRatio)
	}
	return MetaRevisionClassParametric,
		fmt.Sprintf("skeleton unchanged, change ratio %.2f", ratio)
}

func truncateMetaDetail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// MetaRevisionClassBalance aggregates the structural/parametric mix of recent
// meta revisions. AdoptedParametricStreak counts the newest consecutive
// adoptions whose class resolves to parametric; a record whose class cannot be
// resolved (pre-instrumentation history) ends the streak scan — honest zero
// over guessed continuity.
type MetaRevisionClassBalance struct {
	Structural              int `json:"structural"`   // among newest window of proposed revisions
	Parametric              int `json:"parametric"`   //
	Unclassified            int `json:"unclassified"` // proposed before this instrumentation landed
	AdoptedParametricStreak int `json:"adoptedParametricStreak"`
}

// MetaRevisionClassBalance computes the balance from the meta-experience
// ledger. Feed-card adoptions are separate Action records without a class of
// their own; they resolve through the (artifact, toVersion) join against the
// proposal record that produced the version.
func (t *Tracker) MetaRevisionClassBalance() MetaRevisionClassBalance {
	var out MetaRevisionClassBalance
	entries, err := t.RecentMetaRevisions(3 * metaClassBalanceWindow)
	if err != nil || len(entries) == 0 {
		return out
	}
	classByVersion := map[string]string{}
	for _, e := range entries {
		if e.Proposed && e.RevisionClass != "" && e.ToVersion != "" {
			classByVersion[e.Artifact+"|"+e.ToVersion] = e.RevisionClass
		}
	}
	proposals := 0
	streakOpen := true
	for _, e := range entries { // newest first
		if e.Proposed && proposals < metaClassBalanceWindow {
			proposals++
			switch e.RevisionClass {
			case MetaRevisionClassStructural:
				out.Structural++
			case MetaRevisionClassParametric:
				out.Parametric++
			default:
				out.Unclassified++
			}
		}
		if !streakOpen {
			continue
		}
		switch e.Action {
		case "auto_adopted", "adopted":
			cls := e.RevisionClass
			if cls == "" {
				cls = classByVersion[e.Artifact+"|"+e.ToVersion]
			}
			if cls == MetaRevisionClassParametric {
				out.AdoptedParametricStreak++
			} else {
				// Structural adoption or unresolvable class — the streak ends.
				streakOpen = false
			}
		}
	}
	return out
}
