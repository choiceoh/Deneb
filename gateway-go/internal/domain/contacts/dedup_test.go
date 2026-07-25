package contacts

import (
	"sort"
	"testing"
)

func groupContaining(res DedupResult, idx int) *MergeGroup {
	for i := range res.Merges {
		for _, m := range res.Merges[i].Members {
			if m == idx {
				return &res.Merges[i]
			}
		}
	}
	return nil
}

func TestDedupMergesTripleSyncCopies(t *testing.T) {
	// same person, same personal number, name-variant across sources -> one group
	in := []Contact{
		{Name: "박한주", Phones: []string{"010-1111-2222"}},
		{Name: "#박한주 부장(솔라테크)", Phones: []string{"01011112222"}},
		{Name: "박한주 팀장", Phones: []string{"+82 10-1111-2222"}},
	}
	res := Dedup(in)
	if res.Distinct != 1 {
		t.Fatalf("distinct = %d, want 1 (all one person)", res.Distinct)
	}
	g := groupContaining(res, 0)
	if g == nil || len(g.Members) != 3 {
		t.Fatalf("expected a 3-member group, got %+v", res.Merges)
	}
	if g.Canonical != "박한주" {
		t.Errorf("canonical = %q, want 박한주 (cleanest)", g.Canonical)
	}
}

func TestDedupDoubledSyllableCorruption(t *testing.T) {
	in := []Contact{
		{Name: "홍정우", Emails: []string{"hong@x.co"}},
		{Name: "홍홍정우 상무", Emails: []string{"hong@x.co"}},
	}
	if res := Dedup(in); res.Distinct != 1 {
		t.Fatalf("distinct = %d, want 1 (홍홍정우==홍정우)", res.Distinct)
	}
}

func TestDedupSwitchboardNumberNotMerged(t *testing.T) {
	// five different people share one representative line -> no merges, no spam
	shared := "02-345-6000"
	in := []Contact{
		{Name: "김철수", Phones: []string{shared}},
		{Name: "이영희", Phones: []string{shared}},
		{Name: "박민수", Phones: []string{shared}},
		{Name: "최지우", Phones: []string{shared}},
		{Name: "정해인", Phones: []string{shared}},
	}
	res := Dedup(in)
	if res.Distinct != 5 {
		t.Fatalf("distinct = %d, want 5 (switchboard must not merge)", res.Distinct)
	}
	if len(res.Ambiguous) != 0 {
		t.Errorf("switchboard number must not spew ambiguous pairs, got %d", len(res.Ambiguous))
	}
}

func TestDedupAmbiguousWhenNamesDiffer(t *testing.T) {
	// two different names on one personal number -> ambiguous, not merged
	in := []Contact{
		{Name: "강상민", Phones: []string{"010-9999-8888"}},
		{Name: "박태경", Phones: []string{"010-9999-8888"}},
	}
	res := Dedup(in)
	if res.Distinct != 2 {
		t.Fatalf("distinct = %d, want 2 (unresolved -> stay apart)", res.Distinct)
	}
	if len(res.Ambiguous) != 1 {
		t.Fatalf("ambiguous = %d, want 1", len(res.Ambiguous))
	}
	if res.Ambiguous[0].Shared != "01099998888" {
		t.Errorf("shared = %q, want normalized phone", res.Ambiguous[0].Shared)
	}
}

func TestDedupRoleEmailNotAJoinKey(t *testing.T) {
	// two people sharing info@ must not merge and must not be ambiguous
	in := []Contact{
		{Name: "김철수", Emails: []string{"info@corp.com"}},
		{Name: "이영희", Emails: []string{"info@corp.com"}},
	}
	res := Dedup(in)
	if res.Distinct != 2 || len(res.Ambiguous) != 0 {
		t.Fatalf("role email should be ignored: distinct=%d ambiguous=%d", res.Distinct, len(res.Ambiguous))
	}
}

func TestDedupTransitiveWithinNameFamily(t *testing.T) {
	// A~B by phone, B~C by email, same name -> all three collapse
	in := []Contact{
		{Name: "김대희", Phones: []string{"010-2222-3333"}},
		{Name: "김대희 과장", Phones: []string{"010-2222-3333"}, Emails: []string{"kdh@topsolar.kr"}},
		{Name: "김대희", Emails: []string{"kdh@topsolar.kr"}},
	}
	res := Dedup(in)
	if res.Distinct != 1 {
		t.Fatalf("distinct = %d, want 1 (transitive)", res.Distinct)
	}
	if g := groupContaining(res, 0); g == nil || len(g.Members) != 3 {
		t.Fatalf("want one 3-member group, got %+v", res.Merges)
	}
}

func TestDedupEmptyAndSingletons(t *testing.T) {
	if res := Dedup(nil); res.Total != 0 || len(res.Merges) != 0 {
		t.Errorf("empty input must yield empty result, got %+v", res)
	}
	in := []Contact{{Name: "김철수", Phones: []string{"010-1111-1111"}}, {Name: "이영희"}}
	res := Dedup(in)
	if res.Distinct != 2 || len(res.Merges) != 0 {
		t.Errorf("distinct singletons must not merge, got distinct=%d merges=%d", res.Distinct, len(res.Merges))
	}
}

func TestDedupDeterministicOrder(t *testing.T) {
	in := []Contact{
		{Name: "이수", Phones: []string{"010-5555-5555"}},
		{Name: "이수민", Phones: []string{"010-7777-7777"}},
		{Name: "이수", Phones: []string{"010-5555-5555"}},
	}
	first := Dedup(in)
	for i := 0; i < 5; i++ {
		got := Dedup(in)
		if len(got.Merges) != len(first.Merges) {
			t.Fatalf("nondeterministic merge count")
		}
	}
	// "이수" and "이수민" stay distinct (exact-key, no shared id)
	mem := []int{}
	if g := groupContaining(first, 0); g != nil {
		mem = append(mem, g.Members...)
	}
	sort.Ints(mem)
	if len(mem) != 2 || mem[0] != 0 || mem[1] != 2 {
		t.Errorf("이수 copies should merge (0,2), 이수민 stay out; got %v", mem)
	}
}
