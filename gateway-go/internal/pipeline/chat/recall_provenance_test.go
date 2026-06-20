// recall_provenance_test.go — unit tests for applyProvenancePenalty
// (recall_provenance.go). The end-to-end behavior through the real recall
// pipeline is covered by recall_gain_test.go:TestRecallSynthesisDrift; here we
// drive the function directly on constructed recallEvidence so the rule's two
// guards (type-aware, entity-scoped) are checked in isolation.
package chat

import "testing"

// topEvidenceKind returns the Kind of the highest-scored row.
func topEvidenceKind(ev []recallEvidence) string {
	best, kind := -1.0, ""
	for _, e := range ev {
		if e.Score > best {
			best, kind = e.Score, e.Kind
		}
	}
	return kind
}

func TestApplyProvenancePenalty(t *testing.T) {
	wiki := func(note string, score float64) recallEvidence {
		return recallEvidence{Kind: "wiki", Source: "거래/x.md", Note: note, Score: score}
	}
	diary := func(note string, score float64) recallEvidence {
		return recallEvidence{Kind: "diary", Source: "diary#1", Note: note, Score: score}
	}

	cases := []struct {
		name      string
		ev        []recallEvidence
		wantTop   string  // expected top Kind after the penalty
		wantFired bool    // whether any wiki score should be demoted
		wikiAfter float64 // expected wiki score when fired (else ignored)
	}{
		{
			// DRIFT: wiki figure contradicts the diary figure on the same entity →
			// wiki demoted below the raw, raw wins.
			name: "numeric_drift_demoted",
			ev: []recallEvidence{
				wiki("title: 에코프로 케이블 견적 | summary: 에코프로 케이블 2,950m 견적", 1.39),
				diary("에코프로 케이블 1,950m 견적 회신함.", 1.16),
			},
			wantTop: "diary", wantFired: true, wikiAfter: 1.15,
		},
		{
			// PARAPHRASE: vocabulary differs (아침편지 ↔ 모닝레터), no numeric token →
			// untouched, wiki keeps its earned prior.
			name: "paraphrase_untouched",
			ev: []recallEvidence{
				wiki("title: 주간업무보고 자동화 | summary: 모닝레터 패턴 재사용", 1.40),
				diary("주간보고 PDF 자동화는 아침편지 만들던 방식 그대로 쓰기로.", 1.10),
			},
			wantTop: "wiki", wantFired: false,
		},
		{
			// SUPERSESSION: names differ (이태호 → 박수진), no numeric token → untouched.
			name: "names_untouched",
			ev: []recallEvidence{
				wiki("title: 에이콘 상사 담당자 | summary: 박수진 과장", 1.64),
				diary("에이콘 상사 구매 담당자는 이태호 차장.", 1.53),
			},
			wantTop: "wiki", wantFired: false,
		},
		{
			// AGREEMENT: wiki and diary share the SAME figure → no mutual
			// disagreement, no penalty.
			name: "agreeing_figure_untouched",
			ev: []recallEvidence{
				wiki("title: 에코프로 케이블 견적 | summary: 에코프로 케이블 1,950m 견적", 1.70),
				diary("에코프로 케이블 1,950m 견적 회신함.", 1.52),
			},
			wantTop: "wiki", wantFired: false,
		},
		{
			// CROSS-FACT: a wiki figure and a diary figure that disagree but are
			// about DIFFERENT entities (no shared token) must not be paired.
			name: "cross_entity_not_paired",
			ev: []recallEvidence{
				wiki("title: 현대차 울산 모듈 | summary: 550W 2,000장", 1.50),
				diary("에코프로 케이블 1,950m 견적 회신함.", 1.20),
			},
			wantTop: "wiki", wantFired: false,
		},
		{
			// WIKI ADDS A FIGURE the diary never had (not a contradiction): the diary
			// has no number the wiki lacks → not mutual disagreement → untouched.
			name: "wiki_adds_figure_untouched",
			ev: []recallEvidence{
				wiki("title: 에코프로 케이블 | summary: 에코프로 케이블 1,950m 견적", 1.40),
				diary("에코프로 케이블 견적 회신함.", 1.10),
			},
			wantTop: "wiki", wantFired: false,
		},
		{
			// EVOLUTION (NOT drift): the figure genuinely changed and the NEW value
			// WAS recorded in the diary. The wiki's figure is corroborated by that
			// raw entry, so it is NOT demoted even though an OLDER diary entry still
			// disagrees — this is the false positive the corroboration check fixes.
			name: "evolution_corroborated_untouched",
			ev: []recallEvidence{
				wiki("title: 프로젝트 알파 예산 | summary: 프로젝트 알파 예산 5,000만원", 1.50),
				diary("프로젝트 알파 예산 5,000만원으로 증액.", 1.30), // corroborates the wiki figure
				diary("프로젝트 알파 예산 3,000만원으로 잡음.", 1.20), // older, disagrees
			},
			wantTop: "wiki", wantFired: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := make([]float64, len(tc.ev))
			for i, e := range tc.ev {
				before[i] = e.Score
			}
			applyProvenancePenalty(tc.ev)
			fired := false
			for i, e := range tc.ev {
				if e.Score != before[i] {
					fired = true
				}
			}
			if fired != tc.wantFired {
				t.Errorf("fired = %v, want %v (scores %v → %v)", fired, tc.wantFired, before, scoresOf(tc.ev))
			}
			if top := topEvidenceKind(tc.ev); top != tc.wantTop {
				t.Errorf("top = %q, want %q (scores %v)", top, tc.wantTop, scoresOf(tc.ev))
			}
			if tc.wantFired {
				for _, e := range tc.ev {
					if e.Kind == "wiki" && !approxEq(e.Score, tc.wikiAfter, 0.001) {
						t.Errorf("demoted wiki score = %.3f, want ≈%.3f", e.Score, tc.wikiAfter)
					}
				}
			}
		})
	}
}

func scoresOf(ev []recallEvidence) []float64 {
	out := make([]float64, len(ev))
	for i, e := range ev {
		out[i] = e.Score
	}
	return out
}

func approxEq(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}
