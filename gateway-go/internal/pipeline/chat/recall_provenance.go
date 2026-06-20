package chat

import (
	"regexp"
	"strings"
)

// Type-aware, entity-scoped provenance penalty for recall ranking.
//
// The wiki source carries a higher prior than the raw diary (recall_evidence.go:
// 0.80 vs 0.70) because curated metadata is usually the better match surface. But
// when the dreamer mis-synthesizes a fact, the curated wiki value DRIFTS from the
// faithful raw diary, and that prior then ranks the WRONG figure first
// (recall_gain_test.go:TestRecallSynthesisDrift measured a 2,950 drift outranking
// the correct 1,950 raw). This demotes a wiki row whose NUMERIC figure
// contradicts a raw diary row ABOUT THE SAME FACT to just below that raw row — an
// unverified curated figure never outranks the raw observation it should have
// come from. CL-Bench (arXiv:2606.05661) names this "spurious generalization" as
// the central hazard of eager curation.
//
// Two guards keep it from over-firing (validated in recall_provenance_test.go and
// end-to-end in TestRecallSynthesisDrift):
//   - TYPE-aware: numeric contradictions only, so a vocabulary paraphrase
//     (아침편지 ↔ 모닝레터) or a name supersession (이태호 → 박수진) is untouched and
//     the wiki keeps its earned prior.
//   - ENTITY-scoped: compared pairwise on a shared entity token, so a figure in
//     one fact can't "contradict" an unrelated fact's figure in a blended recall.
//
// Operates on recallEvidence.Note (clean content — the ref/score/age live in
// Source and the output formatter, not in Note), so no metadata digit can pose as
// an evidence figure.

// recallNumRe matches a figure of interest: ≥2 digits (with optional thousands
// commas) so a lone stray digit isn't treated as a value — "1,950", "18789".
var recallNumRe = regexp.MustCompile(`\d[\d,]*\d`)

// recallProvenanceStop drops generic template tokens so "shared entity" rests on
// distinctive nouns, not boilerplate every wiki note carries.
var recallProvenanceStop = map[string]struct{}{
	"title": {}, "summary": {}, "tags": {}, "match": {}, "거래": {}, "견적": {},
}

func recallNumbers(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range recallNumRe.FindAllString(s, -1) {
		out[m] = struct{}{}
	}
	return out
}

// recallEntityTokens returns distinctive word tokens (≥2 runes, not numeric, not
// boilerplate) used to decide whether two evidence rows are about the same entity.
func recallEntityTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '가' && r <= '힣') && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z')
	}) {
		f = strings.ToLower(f)
		if len([]rune(f)) < 2 {
			continue
		}
		if _, stop := recallProvenanceStop[f]; stop {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

func recallShareEntity(a, b map[string]struct{}) bool {
	for t := range a {
		if _, ok := b[t]; ok {
			return true
		}
	}
	return false
}

// applyProvenancePenalty demotes a wiki evidence row whose numeric figure
// contradicts the raw diary for the same fact AND is UNCORROBORATED by any raw
// observation, to just below the contradicting diary row — so a drifted curated
// figure cannot outrank the faithful raw one. In place on the slice (scores
// only); final ordering is fixed by the caller's subsequent sort.
//
// Two conditions, both required:
//   - CONTRADICTION (mutual disagreement on a same-entity diary row): the wiki
//     asserts a number that row lacks AND that row records one the wiki lacks, so
//     a wiki page that merely ADDS a figure the diary never had is not penalized.
//   - UNCORROBORATED: NO same-entity diary row contains all of the wiki's
//     numbers. This is what separates DRIFT (a synthesis error — the wiki figure
//     appears in no raw entry) from legitimate EVOLUTION (the value genuinely
//     changed and the new figure WAS recorded in the diary): an evolved figure is
//     corroborated by its own raw entry, so the wiki keeps its rank even though an
//     older diary entry still disagrees.
func applyProvenancePenalty(evidence []recallEvidence) {
	type parsed struct {
		nums   map[string]struct{}
		tokens map[string]struct{}
	}
	p := make([]parsed, len(evidence))
	for i, ev := range evidence {
		if ev.Kind == "wiki" || ev.Kind == "diary" {
			p[i] = parsed{nums: recallNumbers(ev.Note), tokens: recallEntityTokens(ev.Note)}
		}
	}
	contains := func(set, want map[string]struct{}) bool {
		for n := range want {
			if _, ok := set[n]; !ok {
				return false
			}
		}
		return true
	}
	for i := range evidence {
		if evidence[i].Kind != "wiki" || len(p[i].nums) == 0 {
			continue
		}
		corroborated := false
		contradictScore := -1.0
		for j := range evidence {
			if evidence[j].Kind != "diary" || len(p[j].nums) == 0 {
				continue
			}
			if !recallShareEntity(p[i].tokens, p[j].tokens) {
				continue // different fact — a figure here can't speak to there
			}
			if contains(p[j].nums, p[i].nums) {
				corroborated = true // a raw entry records the wiki's figure → verified
				break
			}
			diaryExtra := false
			for n := range p[j].nums {
				if _, ok := p[i].nums[n]; !ok {
					diaryExtra = true
				}
			}
			if diaryExtra && evidence[j].Score > contradictScore {
				contradictScore = evidence[j].Score
			}
		}
		if !corroborated && contradictScore >= 0 && evidence[i].Score >= contradictScore {
			evidence[i].Score = contradictScore - 0.01 // raw wins an uncorroborated conflict
		}
	}
}
