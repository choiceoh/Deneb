package wiki

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func missStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The demand ledger answers "what does the wiki keep failing to answer" — the
// terms, ranked by how often they were asked, so the research lane can target
// real demand instead of pure round-robin staleness.
func TestRecallDemandTerms_RanksAskedTopicsAndDropsScaffolding(t *testing.T) {
	s := missStore(t)
	for _, q := range []string{
		"완도 태양광 진행 상황 알려줘",
		"완도 프로젝트 어디까지 갔지",
		"징코 모듈 단가 뭐야",
	} {
		if err := s.RecordRecallMiss(RecallMiss{Query: q, Session: "client:main"}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	terms := s.RecallDemandTerms(time.Now(), 10)
	if len(terms) == 0 {
		t.Fatal("no demand terms recorded")
	}
	// 완도 was asked twice — it must lead.
	if terms[0] != "완도" {
		t.Errorf("terms[0] = %q, want 완도 (asked twice): %v", terms[0], terms)
	}
	for _, junk := range []string{"진행", "상황", "알려줘", "뭐야", "어디"} {
		for _, got := range terms {
			if got == junk {
				t.Errorf("question scaffolding %q leaked into demand terms: %v", junk, terms)
			}
		}
	}
}

// A blank question records nothing, and the window/limit bound the output —
// the ledger must never grow into the selection path unbounded.
func TestRecallDemandTerms_BoundedByLimitAndWindow(t *testing.T) {
	s := missStore(t)
	if err := s.RecordRecallMiss(RecallMiss{Query: "   "}); err != nil {
		t.Fatalf("blank record: %v", err)
	}
	if got := s.RecallDemandTerms(time.Now(), 10); len(got) != 0 {
		t.Errorf("blank query recorded: %v", got)
	}

	if err := s.RecordRecallMiss(RecallMiss{Query: "완도 징코 한화 신성 현대"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if got := s.RecallDemandTerms(time.Now(), 2); len(got) != 2 {
		t.Errorf("limit ignored: %v", got)
	}
	// Ledger entries older than the demand window are not demand any more.
	future := time.Now().Add(recallMissDemandWindow + 24*time.Hour)
	if got := s.RecallDemandTerms(future, 10); len(got) != 0 {
		t.Errorf("stale misses still counted as demand: %v", got)
	}
}

func TestCompactRecallMisses_DropsPastRetention(t *testing.T) {
	s := missStore(t)
	if err := s.RecordRecallMiss(RecallMiss{Query: "완도 태양광"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	dropped, err := s.compactRecallMisses(time.Now())
	if err != nil || dropped != 0 {
		t.Fatalf("fresh line compacted: dropped=%d err=%v", dropped, err)
	}
	future := time.Now().Add(recallMissRetention + 24*time.Hour)
	dropped, err = s.compactRecallMisses(future)
	if err != nil || dropped != 1 {
		t.Fatalf("retention not enforced: dropped=%d err=%v", dropped, err)
	}
	if got := s.RecallDemandTerms(future, 10); len(got) != 0 {
		t.Errorf("compacted lines still readable: %v", got)
	}
}

// Live probe (2026-07-27) recorded "완도 관산포 프로젝트 진행 상황 기억나?" and the
// first extraction kept 기억나 (recall cue) and 프로젝트 (category word). The
// category word is the harmful one: it appears in every project page, so the
// research lane would see the whole wiki as demanded.
func TestRecallDemandTerms_DropsRecallCuesAndCategoryWords(t *testing.T) {
	s := missStore(t)
	if err := s.RecordRecallMiss(RecallMiss{Query: "완도 관산포 프로젝트 진행 상황 기억나?"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	got := s.RecallDemandTerms(time.Now(), 10)
	want := map[string]bool{"완도": true, "관산포": true}
	for _, term := range got {
		if !want[term] {
			t.Errorf("non-topic term %q survived: %v", term, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("terms = %v, want exactly 완도/관산포", got)
	}
}

// A bare topic word that happens to end in a particle-like syllable must
// survive. 도 is the crux: it is both a particle ("나도") and the final
// syllable of place names (완도, 강원도), so it is excluded from the strip list.
func TestStripTrailingParticles(t *testing.T) {
	cases := []struct{ in, want string }{
		{"완도", "완도"},    // 도 = name syllable, not a particle
		{"강원도", "강원도"},  // administrative 도 stays intact
		{"비금은", "비금"},   // topic particle
		{"완도에서", "완도"},  // locative
		{"완도의", "완도"},   // possessive
		{"완도에서는", "완도"}, // stacked particles unwind
		{"기아를", "기아"},   // object particle (vowel-final stem)
		{"모듈이", "모듈"},   // subject particle (consonant-final stem)
		{"현장으로", "현장"},  // instrumental
		{"완도랑", "완도"},   // comitative
		{"비금부터", "비금"},  // delimiters
		{"완도까지", "완도"},
		{"관산포", "관산포"}, // bare topic untouched
		{"징코", "징코"},
		{"금호타이어", "금호타이어"}, // ends in 어 — not a particle
	}
	for _, c := range cases {
		if got := stripTrailingParticles(c.in); got != c.want {
			t.Errorf("stripTrailingParticles(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The demand ledger must record the topic ("비금"), not its grammatical case
// ("비금은") — a particle-suffixed term would never substring-match a page
// titled with the bare name.
func TestRecallDemandTerms_StripsTrailingParticles(t *testing.T) {
	s := missStore(t)
	for _, q := range []string{
		"비금은 어떻게 됐어",
		"완도에서 진행 상황 알려줘",
		"완도의 모듈 단가 뭐야",
	} {
		if err := s.RecordRecallMiss(RecallMiss{Query: q, Session: "client:main"}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	got := map[string]bool{}
	for _, term := range s.RecallDemandTerms(time.Now(), 20) {
		got[term] = true
	}
	for _, want := range []string{"비금", "완도", "모듈"} {
		if !got[want] {
			t.Errorf("stripped topic %q missing from demand terms: %v", want, got)
		}
	}
	for _, bad := range []string{"비금은", "완도에서", "완도의"} {
		if got[bad] {
			t.Errorf("particle-suffixed term %q leaked into demand terms: %v", bad, got)
		}
	}
}
