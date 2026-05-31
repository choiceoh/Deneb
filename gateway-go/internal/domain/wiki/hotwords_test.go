package wiki

import (
	"os"
	"strings"
	"testing"
)

func TestHotwordHints(t *testing.T) {
	s := &Store{index: NewIndex()}
	s.index.Entries["프로젝트/a.md"] = IndexEntry{
		Title: "에코프로 태양광 사업", Category: "프로젝트", Type: "entity", Importance: 0.5,
		Tags: []string{"에코프로", "탑솔라", "김대희"},
	}
	s.index.Entries["사람/b.md"] = IndexEntry{
		Title: "석문호", Category: "사람", Type: "entity", Importance: 0.7,
		Tags: []string{"석문호", "케이원일렉트릭"},
	}
	out := s.HotwordHints(50)

	for _, want := range []string{"에코프로 태양광 사업", "에코프로", "탑솔라", "김대희", "석문호", "케이원일렉트릭"} {
		if !strings.Contains(out, want) {
			t.Errorf("hotwords missing %q; got %q", want, out)
		}
	}

	// 석문호 is both a title and a tag -> deduped to a single term.
	count := 0
	for _, term := range strings.Split(out, ", ") {
		if term == "석문호" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("석문호 should be deduped to 1, got %d in %q", count, out)
	}

	// Empty wiki -> empty string.
	if got := (&Store{index: NewIndex()}).HotwordHints(50); got != "" {
		t.Errorf("empty wiki hotwords = %q, want empty", got)
	}
}

func TestHotwordHintsCap(t *testing.T) {
	s := &Store{index: NewIndex()}
	s.index.Entries["a.md"] = IndexEntry{Title: "t1", Tags: []string{"x1", "x2", "x3", "x4"}}
	if got := strings.Split(s.HotwordHints(3), ", "); len(got) > 3 {
		t.Errorf("maxTerms=3 exceeded: %v", got)
	}
}

// TestHotwordHints_LiveWiki proves the real index.md parses into the titles/tags
// HotwordHints reads. NewStore may rewrite the index, so point DENEB_WIKI_DIR at
// a COPY of the wiki, not the live one:
//
//	cp -r ~/.deneb/wiki /tmp/wiki-copy
//	DENEB_WIKI_LIVE=1 DENEB_WIKI_DIR=/tmp/wiki-copy \
//	  go test -run TestHotwordHints_LiveWiki -v ./internal/domain/wiki/
func TestHotwordHints_LiveWiki(t *testing.T) {
	if os.Getenv("DENEB_WIKI_LIVE") != "1" {
		t.Skip("set DENEB_WIKI_LIVE=1 + DENEB_WIKI_DIR (a copy of the wiki) to run")
	}
	dir := os.Getenv("DENEB_WIKI_DIR")
	if dir == "" {
		t.Skip("set DENEB_WIKI_DIR to a wiki directory copy")
	}
	s, err := NewStore(dir, t.TempDir())
	if err != nil {
		t.Fatalf("open wiki %q: %v", dir, err)
	}
	defer s.Close()
	out := s.HotwordHints(200)
	t.Logf("hotwords (%d chars):\n%s", len(out), out)
}
