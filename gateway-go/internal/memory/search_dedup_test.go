package memory

import (
	"context"
	"math"
	"testing"
)

func TestJaccardTokenize(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect map[string]struct{}
	}{
		{
			name:  "english lowercase",
			input: "Hello World 123",
			expect: map[string]struct{}{
				"hello": {}, "world": {}, "123": {},
			},
		},
		{
			name:  "korean tokens",
			input: "메모리 검색 최적화",
			expect: map[string]struct{}{
				"메모리": {}, "검색": {}, "최적화": {},
			},
		},
		{
			name:  "mixed korean and english",
			input: "Dreaming 자가개선 패턴",
			expect: map[string]struct{}{
				"dreaming": {}, "자가개선": {}, "패턴": {},
			},
		},
		{
			name:   "empty string",
			input:  "",
			expect: map[string]struct{}{},
		},
		{
			name:   "only punctuation",
			input:  "!@#$%^&*()",
			expect: map[string]struct{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardTokenize(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("jaccardTokenize(%q): got %d tokens %v, want %d tokens %v",
					tt.input, len(got), got, len(tt.expect), tt.expect)
			}
			for tok := range tt.expect {
				if _, ok := got[tok]; !ok {
					t.Errorf("missing token %q in result %v", tok, got)
				}
			}
		})
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]struct{}
		want float64
	}{
		{
			name: "identical sets",
			a:    map[string]struct{}{"hello": {}, "world": {}},
			b:    map[string]struct{}{"hello": {}, "world": {}},
			want: 1.0,
		},
		{
			name: "disjoint sets",
			a:    map[string]struct{}{"hello": {}, "world": {}},
			b:    map[string]struct{}{"foo": {}, "bar": {}},
			want: 0.0,
		},
		{
			name: "partial overlap",
			a:    map[string]struct{}{"hello": {}, "world": {}, "foo": {}},
			b:    map[string]struct{}{"hello": {}, "world": {}, "bar": {}},
			want: 0.5, // intersection=2, union=4
		},
		{
			name: "both empty",
			a:    map[string]struct{}{},
			b:    map[string]struct{}{},
			want: 0.0,
		},
		{
			name: "one empty",
			a:    map[string]struct{}{"hello": {}},
			b:    map[string]struct{}{},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-10 {
				t.Errorf("jaccardSimilarity = %.4f, want %.4f", got, tt.want)
			}
		})
	}
}

func TestDedupResults_RemovesNearDuplicates(t *testing.T) {
	results := []SearchResult{
		{Fact: Fact{ID: 1, Content: "Dreaming 자가개선 패턴으로 메모리 품질 향상"}, Score: 0.9},
		{Fact: Fact{ID: 2, Content: "Dreaming 자가개선 패턴을 통한 메모리 품질 개선"}, Score: 0.85},
		{Fact: Fact{ID: 3, Content: "노이즈 최소화를 위한 검색 전략"}, Score: 0.8},
	}

	deduped := dedupResults(results, dedupJaccardThreshold)

	// Result 2 is near-duplicate of result 1; should be removed.
	if len(deduped) != 2 {
		t.Fatalf("expected 2 results after dedup, got %d", len(deduped))
	}
	if deduped[0].Fact.ID != 1 {
		t.Errorf("first result should be fact 1, got %d", deduped[0].Fact.ID)
	}
	if deduped[1].Fact.ID != 3 {
		t.Errorf("second result should be fact 3, got %d", deduped[1].Fact.ID)
	}
}

func TestDedupResults_PreservesUnique(t *testing.T) {
	results := []SearchResult{
		{Fact: Fact{ID: 1, Content: "인증 토큰은 Redis에 저장"}, Score: 0.9},
		{Fact: Fact{ID: 2, Content: "파일 캐시 전략 업데이트 필요"}, Score: 0.8},
		{Fact: Fact{ID: 3, Content: "Telegram 메시지 4096자 제한"}, Score: 0.7},
	}

	deduped := dedupResults(results, dedupJaccardThreshold)

	if len(deduped) != 3 {
		t.Fatalf("expected all 3 unique results preserved, got %d", len(deduped))
	}
}

func TestDedupResults_Empty(t *testing.T) {
	deduped := dedupResults(nil, dedupJaccardThreshold)
	if len(deduped) != 0 {
		t.Fatalf("expected empty result, got %d", len(deduped))
	}

	deduped = dedupResults([]SearchResult{}, dedupJaccardThreshold)
	if len(deduped) != 0 {
		t.Fatalf("expected empty result for empty slice, got %d", len(deduped))
	}
}

func TestDedupResults_SingleResult(t *testing.T) {
	results := []SearchResult{
		{Fact: Fact{ID: 1, Content: "단일 팩트"}, Score: 0.9},
	}

	deduped := dedupResults(results, dedupJaccardThreshold)
	if len(deduped) != 1 {
		t.Fatalf("expected 1 result, got %d", len(deduped))
	}
}

func TestDedupResults_KoreanNearDuplicates(t *testing.T) {
	// Simulates the real problem: same Korean concept extracted multiple times.
	results := []SearchResult{
		{Fact: Fact{ID: 1, Content: "노이즈 최소화 전략으로 검색 품질 향상 필요"}, Score: 0.9},
		{Fact: Fact{ID: 2, Content: "검색 품질 향상을 위한 노이즈 최소화 전략 적용"}, Score: 0.87},
		{Fact: Fact{ID: 3, Content: "노이즈 최소화 전략 검색 품질 개선 방안"}, Score: 0.83},
		{Fact: Fact{ID: 4, Content: "GPU 추론 성능 최적화 설정"}, Score: 0.75},
	}

	deduped := dedupResults(results, dedupJaccardThreshold)

	// Facts 2 and 3 are near-duplicates of fact 1; fact 4 is distinct.
	if len(deduped) != 2 {
		t.Fatalf("expected 2 results (1 unique cluster + 1 distinct), got %d: %v",
			len(deduped), factIDs(deduped))
	}
	if deduped[0].Fact.ID != 1 {
		t.Errorf("first result should be highest-scored fact 1, got %d", deduped[0].Fact.ID)
	}
	if deduped[1].Fact.ID != 4 {
		t.Errorf("second result should be distinct fact 4, got %d", deduped[1].Fact.ID)
	}
}

func TestDedupResults_IntegrationWithSearch(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	// Insert near-duplicate facts.
	s.InsertFact(ctx, Fact{Content: "Dreaming 패턴으로 자가개선 메모리 품질 향상", Category: CategoryDecision, Importance: 0.9})
	s.InsertFact(ctx, Fact{Content: "Dreaming 패턴 자가개선을 통해 메모리 품질 개선", Category: CategoryDecision, Importance: 0.85})
	s.InsertFact(ctx, Fact{Content: "Dreaming 패턴 자가개선 메모리 품질 향상 방법", Category: CategoryDecision, Importance: 0.8})
	// Insert a distinct fact.
	s.InsertFact(ctx, Fact{Content: "Telegram 봇 API 메시지 길이 제한 4096자", Category: CategorySolution, Importance: 0.7})

	results, err := s.SearchFacts(ctx, "Dreaming 자가개선", nil, SearchOpts{Limit: 10})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}

	// Should collapse the 3 near-duplicate Dreaming facts into 1.
	dreamingCount := 0
	for _, r := range results {
		tokens := jaccardTokenize(r.Fact.Content)
		if _, ok := tokens["dreaming"]; ok {
			dreamingCount++
		}
	}
	if dreamingCount > 1 {
		t.Errorf("expected at most 1 Dreaming fact after dedup, got %d", dreamingCount)
	}
}

// factIDs is a test helper to extract fact IDs from results for error messages.
func factIDs(results []SearchResult) []int64 {
	ids := make([]int64, len(results))
	for i, r := range results {
		ids[i] = r.Fact.ID
	}
	return ids
}
