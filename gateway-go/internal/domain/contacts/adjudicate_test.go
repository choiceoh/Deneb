package contacts

import (
	"context"
	"errors"
	"testing"
)

// fakeAdjudicator returns canned verdicts (one per pair, in order) and records
// how many pairs it was asked to judge.
type fakeAdjudicator struct {
	verdicts []Verdict
	err      error
	seen     int
}

func (f *fakeAdjudicator) Adjudicate(_ context.Context, _ []Contact, pairs []AmbiguousPair) ([]Verdict, error) {
	f.seen = len(pairs)
	if f.err != nil {
		return nil, f.err
	}
	return f.verdicts, nil
}

// oneAmbiguousPair: two people share a personal number under incompatible names.
func oneAmbiguousPair() []Contact {
	return []Contact{
		{Name: "강상민", Phones: []string{"010-9999-8888"}},
		{Name: "브라이트강상민", Phones: []string{"010-9999-8888"}},
	}
}

func TestResolveSameFoldsPair(t *testing.T) {
	in := oneAmbiguousPair()
	res := Dedup(in)
	if len(res.Ambiguous) != 1 || res.Distinct != 2 {
		t.Fatalf("precondition: ambiguous=%d distinct=%d, want 1/2", len(res.Ambiguous), res.Distinct)
	}
	adj := &fakeAdjudicator{verdicts: []Verdict{VerdictSame}}
	got, err := res.Resolve(context.Background(), in, adj)
	if err != nil {
		t.Fatal(err)
	}
	if adj.seen != 1 {
		t.Errorf("adjudicator saw %d pairs, want 1", adj.seen)
	}
	if got.Distinct != 1 || len(got.Merges) != 1 || len(got.Ambiguous) != 0 {
		t.Fatalf("SAME should merge: distinct=%d merges=%d ambiguous=%d", got.Distinct, len(got.Merges), len(got.Ambiguous))
	}
	// original result is untouched
	if res.Distinct != 2 {
		t.Errorf("Resolve mutated the receiver (distinct=%d)", res.Distinct)
	}
}

func TestResolveDifferentAndUnsureStayApart(t *testing.T) {
	for _, v := range []Verdict{VerdictDifferent, VerdictUnsure} {
		in := oneAmbiguousPair()
		res := Dedup(in)
		got, err := res.Resolve(context.Background(), in, &fakeAdjudicator{verdicts: []Verdict{v}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Distinct != 2 || len(got.Merges) != 0 || len(got.Ambiguous) != 1 {
			t.Fatalf("verdict %d must keep apart: distinct=%d merges=%d ambiguous=%d",
				v, got.Distinct, len(got.Merges), len(got.Ambiguous))
		}
	}
}

func TestResolveSameJoinsTwoMergedFamilies(t *testing.T) {
	// [0,1] and [2,3] each merge deterministically; a shared personal number links
	// 강상민 and 브라이트강상민 as ambiguous — SAME must collapse all four.
	in := []Contact{
		{Name: "강상민", Phones: []string{"010-1111-1111", "010-3333-3333"}},
		{Name: "강상민 회장", Phones: []string{"010-1111-1111"}},
		{Name: "브라이트강상민", Phones: []string{"010-2222-2222", "010-3333-3333"}},
		{Name: "브라이트강상민 대표", Phones: []string{"010-2222-2222"}},
	}
	res := Dedup(in)
	if res.Distinct != 2 || len(res.Ambiguous) != 1 {
		t.Fatalf("precondition: distinct=%d ambiguous=%d, want 2/1", res.Distinct, len(res.Ambiguous))
	}
	got, err := res.Resolve(context.Background(), in, &fakeAdjudicator{verdicts: []Verdict{VerdictSame}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Distinct != 1 || len(got.Merges) != 1 || len(got.Merges[0].Members) != 4 {
		t.Fatalf("SAME across families should collapse to one 4-member group: %+v", got.Merges)
	}
}

func TestResolveNilAndEmptyAreNoops(t *testing.T) {
	in := oneAmbiguousPair()
	res := Dedup(in)
	if got, _ := res.Resolve(context.Background(), in, nil); got.Distinct != res.Distinct {
		t.Errorf("nil adjudicator must be a no-op")
	}
	// a book with no ambiguous pairs: adjudicator must not even be called
	clean := []Contact{{Name: "김철수", Phones: []string{"010-1111-2222"}}}
	adj := &fakeAdjudicator{err: errors.New("must not be called")}
	if _, err := Dedup(clean).Resolve(context.Background(), clean, adj); err != nil {
		t.Errorf("empty ambiguous must skip the adjudicator, got %v", err)
	}
}

func TestResolvePropagatesAdjudicatorError(t *testing.T) {
	in := oneAmbiguousPair()
	res := Dedup(in)
	sentinel := errors.New("backend down")
	got, err := res.Resolve(context.Background(), in, &fakeAdjudicator{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error not propagated: %v", err)
	}
	if got.Distinct != res.Distinct {
		t.Errorf("on error, result should be the unresolved input")
	}
}

func TestResolveMissingVerdictsDefaultUnsure(t *testing.T) {
	// adjudicator returns fewer verdicts than pairs -> the rest default to Unsure
	in := oneAmbiguousPair()
	res := Dedup(in)
	got, err := res.Resolve(context.Background(), in, &fakeAdjudicator{verdicts: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got.Distinct != 2 || len(got.Ambiguous) != 1 {
		t.Errorf("missing verdict must default to keeping apart: distinct=%d", got.Distinct)
	}
}
