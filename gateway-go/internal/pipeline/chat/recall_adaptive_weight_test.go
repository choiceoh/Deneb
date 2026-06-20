// recall_adaptive_weight_test.go — prototype + measurement for SITUATIONAL
// wiki/diary weighting, in answer to the synthesis-drift finding
// (recall_gain_test.go:TestRecallSynthesisDrift). The recall blend uses a FIXED
// source prior — wiki 0.80, diary 0.70 (recall_evidence.go) — which ranks a
// DRIFTED curated figure above the faithful raw one.
//
// The proposed situational rule is a TYPE-AWARE, ENTITY-SCOPED PROVENANCE
// penalty: a wiki row whose NUMERIC figure contradicts a raw diary row ABOUT THE
// SAME FACT (the two rows share an entity term, the wiki asserts a number the
// diary lacks, and the diary records a number the wiki lacks) is demoted just
// below that raw row — an unverified curated figure never outranks the raw
// observation it should have come from. Two guards keep it from over-firing:
//   - TYPE-aware: only numeric contradictions, so a vocabulary paraphrase
//     (아침편지 ↔ 모닝레터) or a name-only supersession (이태호 → 박수진) is untouched.
//   - ENTITY-scoped: compared pairwise on shared entity tokens, so a figure in
//     one fact can't "contradict" an unrelated fact's figure in a blended recall.
//
// This is a TEST-LOCAL rescore over the real recall output — no production change.
// The measurement decides whether recall_evidence.go should adopt the rule: does
// it flip the drift WITHOUT regressing the cases where the wiki prior is earned?
package chat

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// numTokenRe matches a figure of interest (≥2 chars so a lone stray digit isn't
// treated as a value): "1,950", "2,950", "18789", "2,000".
var numTokenRe = regexp.MustCompile(`\d[\d,]*\d`)

var queryEchoRe = regexp.MustCompile(`query="[^"]*"`)

// entityStop drops generic recall/template tokens so "shared entity" rests on
// distinctive nouns, not boilerplate every row carries.
var entityStop = map[string]struct{}{
	"source": {}, "wiki": {}, "diary": {}, "confidence": {}, "high": {}, "score": {},
	"title": {}, "summary": {}, "tags": {}, "match": {}, "age": {}, "ref": {},
	"unknown": {}, "거래": {}, "견적": {},
}

type rankedRow struct {
	kind     string // "wiki" | "diary" | "other"
	score    float64
	adjusted float64
	nums     map[string]struct{}
	tokens   map[string]struct{}
}

func numbersIn(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range numTokenRe.FindAllString(s, -1) {
		out[m] = struct{}{}
	}
	return out
}

// sigTokens returns distinctive word tokens (≥2 runes, not numeric, not
// boilerplate) used to decide whether two rows are about the same entity.
func sigTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= '가' && r <= '힣') && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z')
	}) {
		f = strings.ToLower(f)
		if len([]rune(f)) < 2 {
			continue
		}
		if _, stop := entityStop[f]; stop {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

func shareEntity(a, b map[string]struct{}) bool {
	for t := range a {
		if _, ok := b[t]; ok {
			return true
		}
	}
	return false
}

// parseRanked turns a recall dump into per-source evidence rows carrying score,
// numeric tokens, and entity tokens (the query="..." echo is stripped first, so
// a figure in the question can't masquerade as evidence).
func parseRanked(out string) []rankedRow {
	var rows []rankedRow
	for _, block := range recallRows(out) {
		if !strings.Contains(block, "- source=") {
			continue
		}
		kind := "other"
		switch {
		case strings.Contains(block, "source=wiki"):
			kind = "wiki"
		case strings.Contains(block, "source=diary"):
			kind = "diary"
		}
		score := 0.0
		if i := strings.Index(block, "score="); i >= 0 {
			field := strings.TrimSpace(strings.SplitN(block[i+len("score="):], " ", 2)[0])
			score, _ = strconv.ParseFloat(field, 64)
		}
		// Numbers/tokens come from the NOTE CONTENT ONLY. Drop the "- source=...
		// ref=... age=... score=..." header line first, or the ref's date/time
		// (diary-2026-06-15.md#18:07) and the score (1.70→70) would masquerade as
		// evidence figures and manufacture spurious contradictions.
		content := block
		if nl := strings.IndexByte(block, '\n'); nl >= 0 {
			content = block[nl+1:]
		}
		content = queryEchoRe.ReplaceAllString(content, "")
		rows = append(rows, rankedRow{
			kind: kind, score: score, adjusted: score,
			nums: numbersIn(content), tokens: sigTokens(content),
		})
	}
	return rows
}

// adaptiveProvenanceRescore is the prototype rule (see file header). It compares
// each wiki row PAIRWISE against each diary row and demotes the wiki row just
// below a same-entity diary row that numerically contradicts it. Returns the
// rows sorted by adjusted score. demoted reports whether any demotion fired.
func adaptiveProvenanceRescore(rows []rankedRow) (out []rankedRow, demoted bool) {
	for i := range rows {
		w := &rows[i]
		if w.kind != "wiki" || len(w.nums) == 0 {
			continue
		}
		for j := range rows {
			d := &rows[j]
			if d.kind != "diary" || len(d.nums) == 0 {
				continue
			}
			if !shareEntity(w.tokens, d.tokens) {
				continue // different fact — a figure here can't contradict there
			}
			wikiExtra, diaryExtra := false, false
			for n := range w.nums {
				if _, ok := d.nums[n]; !ok {
					wikiExtra = true
				}
			}
			for n := range d.nums {
				if _, ok := w.nums[n]; !ok {
					diaryExtra = true
				}
			}
			if wikiExtra && diaryExtra && w.adjusted >= d.score {
				w.adjusted = d.score - 0.01 // raw observation wins a numeric conflict
				demoted = true
			}
		}
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].adjusted > rows[b].adjusted })
	return rows, demoted
}

// topKind reports the source of the highest-(adjusted-)scored row.
func topKind(rows []rankedRow, useAdjusted bool) string {
	best, bestKind := -1.0, ""
	for _, r := range rows {
		s := r.score
		if useAdjusted {
			s = r.adjusted
		}
		if s > best {
			best, bestKind = s, r.kind
		}
	}
	return bestKind
}

// TestRecallAdaptiveProvenanceWeighting measures the proposed situational rule on
// all three axes. The win condition: it must flip the DRIFT (raw on top) while
// being a NO-OP on the earned-prior cases (paraphrase, stale, agreeing
// redundancy) — proving it fixes spurious generalization without costing gain.
func TestRecallAdaptiveProvenanceWeighting(t *testing.T) {
	// --- DRIFT: numeric contradiction on the same entity → raw must win.
	driftRows := parseRanked(recallOnce(seedDriftStore(t), driftQuery))
	beforeTop := topKind(driftRows, false)
	_, fired := adaptiveProvenanceRescore(driftRows)
	afterTop := topKind(driftRows, true)
	fmt.Printf("RECALL_ADAPTIVE_DRIFT before_top=%s after_top=%s fired=%v\n", beforeTop, afterTop, fired)
	if beforeTop != "wiki" {
		t.Errorf("fixture sanity: expected the drift (wiki) to win under the fixed prior, got %q", beforeTop)
	}
	if !fired || afterTop != "diary" {
		t.Errorf("adaptive rule failed to put the faithful raw diary on top of the drift (fired=%v after_top=%q)", fired, afterTop)
	}

	// --- NO-OP guards: the rule must NOT fire on the earned-prior cases.
	noop := func(name, query string) {
		t.Helper()
		rows := parseRanked(recallOnce(buildGainStore(t, "both"), query))
		before := topKind(rows, false)
		_, fired := adaptiveProvenanceRescore(rows)
		after := topKind(rows, true)
		fmt.Printf("RECALL_ADAPTIVE_NOOP case=%s fired=%v top=%s\n", name, fired, after)
		if fired || after != before {
			t.Errorf("%s: adaptive rule must be a no-op (no same-entity numeric contradiction), but fired=%v top %q→%q", name, fired, before, after)
		}
	}
	// paraphrase: 아침편지 ↔ 모닝레터 — vocabulary, no numeric conflict.
	noop("paraphrase", "주간보고 자동화 어떤 방식 재사용하기로 했지?")
	// stale-belief: 이태호 → 박수진 — names, no numeric conflict.
	noop("stale", "에이콘 상사 지금 구매 담당자 누구지?")
	// agreeing redundancy: wiki and diary share the SAME figure (1,950) — match,
	// so the mutual-disagreement test is false and nothing is demoted.
	noop("agree-numeric", "에코프로 케이블 견적 몇 미터였지?")
}
