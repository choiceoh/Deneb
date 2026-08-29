package textsearch

import "testing"

// Conjunction rarity semantics: a near-unique co-occurrence of two common
// tokens reads rare; tokens that never co-occur read 0 (every lexical hit
// matched only a subset — the common-word leak shape); a single-token query
// equals that token's own rarity, so single-token gating is unchanged.
func TestQueryConjunctionRarity(t *testing.T) {
	idx := New()
	for i := 0; i < 30; i++ {
		idx.Upsert(fmtID("a", i), "월말 정산 자료")
		idx.Upsert(fmtID("b", i), "주간 회의 자료")
	}
	idx.Upsert("both", "정산 회의 병합 문서")

	if r := idx.QueryConjunctionRarity("정산 회의"); r < 0.9 {
		t.Fatalf("near-unique co-occurrence must read rare, got %.3f", r)
	}
	if r := idx.QueryConjunctionRarity("월말 주간"); r != 0 {
		t.Fatalf("never-co-occurring commons must read 0, got %.3f", r)
	}
	single := idx.QueryMaxRarity("자료")
	if r := idx.QueryConjunctionRarity("자료"); r != single {
		t.Fatalf("single-token conjunction must equal the token rarity: %.3f vs %.3f", r, single)
	}
}

func fmtID(prefix string, i int) string {
	return prefix + string(rune('0'+i/10)) + string(rune('0'+i%10))
}
