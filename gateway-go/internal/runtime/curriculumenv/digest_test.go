package curriculumenv

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// fakeFeed is an in-memory FeedLister so the digest test needs no real store.
type fakeFeed struct {
	items []workfeed.Item
	err   error
}

func (f fakeFeed) List(int, bool) ([]workfeed.Item, int, error) {
	return f.items, len(f.items), f.err
}

// fakeWiki is an in-memory WikiDomainSource.
type fakeWiki map[string]struct{}

func (f fakeWiki) ActiveCounterpartyDomains(string) map[string]struct{} { return f }

// truncRunes caps at n runes with an ellipsis (Korean runes count as 1 each).
func TestTruncRunes(t *testing.T) {
	if got := truncRunes("abc", 5); got != "abc" {
		t.Fatalf("short string should be unchanged: got %q", got)
	}
	got := truncRunes("abcdef", 4)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 4 {
		t.Fatalf("truncRunes(6,4) = %q, want 4 runes ending …", got)
	}
	got = truncRunes("한글테스트데이터", 5)
	if len([]rune(got)) != 5 {
		t.Fatalf("Korean truncRunes = %q, want 5 runes", got)
	}
}

// The digest surfaces recent feed-item titles, skipping blank ones.
func TestDigest_FeedItems(t *testing.T) {
	feed := fakeFeed{items: []workfeed.Item{
		{Title: "계약 검토 — NDA 초안"}, {Title: "  "}, {Title: "주간 보고서 작성"},
	}}
	got := Digest(Sources{Feed: feed})
	if !strings.Contains(got, "계약 검토") || !strings.Contains(got, "주간 보고서") {
		t.Fatalf("digest missing a feed title:\n%s", got)
	}
	if !strings.Contains(got, "업무 피드") {
		t.Fatalf("digest missing section header:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != 2 {
		t.Fatalf("expected 2 titled bullets (blank dropped), got %d:\n%s", n, got)
	}
}

// The digest surfaces active wiki domains, sorted, under the section header.
func TestDigest_WikiDomains(t *testing.T) {
	got := Digest(Sources{Wiki: fakeWiki{"acme.com": {}, "bohae.co.kr": {}}})
	if !strings.Contains(got, "위키 상대 도메인") {
		t.Fatalf("digest missing wiki section:\n%s", got)
	}
	// Sorted deterministically.
	if !strings.Contains(got, "acme.com · bohae.co.kr") {
		t.Fatalf("wiki domains not sorted/joined as expected:\n%s", got)
	}
}

// A feed read error drops the feed section without failing the whole digest.
func TestDigest_FeedErrorDropsSection(t *testing.T) {
	got := Digest(Sources{Feed: fakeFeed{err: errBoom}, Wiki: fakeWiki{"x.com": {}}})
	if strings.Contains(got, "업무 피드") {
		t.Fatalf("feed error should drop the feed section:\n%s", got)
	}
	if !strings.Contains(got, "위키 상대 도메인") {
		t.Fatalf("a feed error must not suppress the wiki section:\n%s", got)
	}
}

// No sources wired (dev/test) → empty digest, quiet.
func TestDigest_Empty(t *testing.T) {
	if got := Digest(Sources{}); got != "" {
		t.Fatalf("empty sources should yield empty digest, got %q", got)
	}
}

// The injected clock feeds the wiki cutoff (a fixed Now keeps the test
// deterministic and proves Now is honored).
func TestDigest_InjectedClock(t *testing.T) {
	var gotCutoff string
	src := Sources{
		Wiki: cutoffCapture{&gotCutoff},
		Now:  func() time.Time { return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) },
	}
	Digest(src)
	if gotCutoff != "2026-07-06" { // 2026-07-13 minus the 7d window
		t.Fatalf("cutoff = %q, want 2026-07-06 (Now - 7d)", gotCutoff)
	}
}

type cutoffCapture struct{ got *string }

func (c cutoffCapture) ActiveCounterpartyDomains(cutoff string) map[string]struct{} {
	*c.got = cutoff
	return nil
}

var errBoom = fakeErr("boom")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }
