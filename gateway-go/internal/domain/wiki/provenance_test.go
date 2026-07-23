package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewEpisodeRefIsDeterministicAndDated(t *testing.T) {
	ref := newEpisodeRef("2026-07-23", "다이어리 원본 내용")
	if !strings.HasPrefix(ref, "d2026-07-23#") {
		t.Fatalf("ref = %q; want d<date>#<hash> prefix", ref)
	}
	// Same span → same ref (content-addressed, dedupes re-processing).
	if again := newEpisodeRef("2026-07-23", "다이어리 원본 내용"); again != ref {
		t.Fatalf("ref not deterministic: %q vs %q", ref, again)
	}
	// Different span → different digest.
	if other := newEpisodeRef("2026-07-23", "다른 내용"); other == ref {
		t.Fatalf("distinct spans collided on %q", ref)
	}
	// No diary date still yields a usable, resolvable-by-hash ref.
	if bare := newEpisodeRef("", "내용"); !strings.HasPrefix(bare, "ep-") {
		t.Fatalf("dateless ref = %q; want ep- prefix", bare)
	}
	// Nothing to attribute → no hollow ref.
	if empty := newEpisodeRef("2026-07-23", "   "); empty != "" {
		t.Fatalf("empty span produced ref %q; want \"\"", empty)
	}
	// The token must survive the flow-array form (no comma/space).
	if strings.ContainsAny(ref, ", ") {
		t.Fatalf("ref %q contains a flow-array-unsafe char", ref)
	}
}

func TestNormalizeSourcesDedupesAndKeepsNewestWindow(t *testing.T) {
	in := []string{" a ", "a", "b", "", "c"}
	got := normalizeSources(in)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("normalizeSources(%v) = %v; want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeSources = %v; want %v", got, want)
		}
	}

	// Over the cap: the oldest are dropped, newest window kept.
	over := make([]string, 0, maxSources+3)
	for i := 0; i < maxSources+3; i++ {
		over = append(over, string(rune('A'+i)))
	}
	capped := normalizeSources(over)
	if len(capped) != maxSources {
		t.Fatalf("cap not enforced: len = %d; want %d", len(capped), maxSources)
	}
	if capped[len(capped)-1] != over[len(over)-1] {
		t.Fatalf("newest episode dropped: tail = %q; want %q", capped[len(capped)-1], over[len(over)-1])
	}
}

func TestAppendEpisodeIgnoresEmpty(t *testing.T) {
	base := []string{"a"}
	if got := appendEpisode(base, ""); len(got) != 1 || got[0] != "a" {
		t.Fatalf("appendEpisode with empty ref mutated list: %v", got)
	}
	if got := appendEpisode(base, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("appendEpisode did not append: %v", got)
	}
}

func TestParseEpisodeRefInvertsNewEpisodeRefAndRejectsMalformed(t *testing.T) {
	ref := newEpisodeRef("2026-07-23", "원본 내용")
	date, hash, ok := parseEpisodeRef(ref)
	if !ok || date != "2026-07-23" || hash == "" {
		t.Fatalf("parse(%q) = (%q,%q,%v); want date 2026-07-23, ok", ref, date, hash, ok)
	}
	// Dateless form.
	if d, h, ok := parseEpisodeRef("ep-deadbeef"); !ok || d != "" || h != "deadbeef" {
		t.Fatalf("dateless parse = (%q,%q,%v)", d, h, ok)
	}
	// Path-traversal date must NOT parse (it would name a file downstream).
	if _, _, ok := parseEpisodeRef("d../../etc#00000000"); ok {
		t.Fatal("traversal date parsed as valid")
	}
	for _, bad := range []string{"", "garbage", "d2026-07-23", "d#abc", "d2026-7-3#abc", "ep-"} {
		if _, _, ok := parseEpisodeRef(bad); ok {
			t.Errorf("parseEpisodeRef(%q) unexpectedly ok", bad)
		}
	}
}

func TestResolveEpisodeLocatesDiaryAndFlagsMissing(t *testing.T) {
	dir := t.TempDir()
	diaryDir := filepath.Join(dir, "diary")
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(dir, "wiki"), diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := os.WriteFile(filepath.Join(diaryDir, "diary-2026-07-20.md"), []byte("일지 원문"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Present diary → resolved + Exists.
	got := store.ResolveEpisode("d2026-07-20#aabbccdd")
	if got.Date != "2026-07-20" || got.DiaryFile != "diary-2026-07-20.md" || !got.Exists || got.Malformed {
		t.Fatalf("present resolve = %+v", got)
	}
	// Missing diary (rotated) → resolved but Exists=false.
	if gone := store.ResolveEpisode("d2026-01-01#00000000"); gone.Exists || gone.Malformed || gone.Date != "2026-01-01" {
		t.Fatalf("missing resolve = %+v", gone)
	}
	// Malformed ref → flagged, no path built.
	if bad := store.ResolveEpisode("not-a-ref"); !bad.Malformed || bad.DiaryFile != "" {
		t.Fatalf("malformed resolve = %+v", bad)
	}
	// Dateless ref → no diary file, not malformed.
	if dl := store.ResolveEpisode("ep-deadbeef"); dl.Malformed || dl.Date != "" || dl.DiaryFile != "" {
		t.Fatalf("dateless resolve = %+v", dl)
	}
}

// TestSourcesRoundTripThroughRenderAndParse guards the frontmatter symmetry:
// a provenance list must survive Render → parse unchanged, or the graph would
// read stale/empty provenance after the next page reopen.
func TestSourcesRoundTripThroughRenderAndParse(t *testing.T) {
	page := NewPage("Provenance Page", "업무", nil)
	page.Meta.Sources = []string{"d2026-07-20#aabbccdd", "d2026-07-23#11223344"}

	reparsed, err := parsePage(page.Render())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	got := reparsed.Meta.Sources
	if len(got) != 2 || got[0] != "d2026-07-20#aabbccdd" || got[1] != "d2026-07-23#11223344" {
		t.Fatalf("sources did not round-trip: %v", got)
	}
}
