package wikiwork

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func seedProjectWithSite(t *testing.T, store *wiki.Store, project, site string) {
	t.Helper()
	rep := &wiki.Page{
		Meta: wiki.Frontmatter{Title: project, Category: "프로젝트", Sites: []string{site}},
		Body: "## 현재 상태\n",
	}
	if err := store.WritePage(wiki.RepPagePath(project), rep); err != nil {
		t.Fatalf("seed rep: %v", err)
	}
}

func TestSiteVisitRecordMatchAndDedup(t *testing.T) {
	store := newTestStore(t)
	seedProjectWithSite(t, store, "수산리태양광", "전북 군산시 옥구읍 수산리")

	dir := t.TempDir()
	r := NewSiteVisitRecorder(store, testWikiLogger(), filepath.Join(dir, "state.json"))

	// A geocoded place matching the site records a visit.
	r.Record("전라북도 군산시 옥구읍 수산리")
	log, err := store.ReadPage(wiki.LogPagePath("수산리태양광"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(log.Body, "방문 |") {
		t.Fatalf("visit not logged: %q", log.Body)
	}
	firstLen := len(log.Body)

	// Same day, same project → deduped (no second entry).
	r.Record("전라북도 군산시 옥구읍 수산리")
	log2, _ := store.ReadPage(wiki.LogPagePath("수산리태양광"))
	if len(log2.Body) != firstLen {
		t.Errorf("duplicate visit written: %d → %d", firstLen, len(log2.Body))
	}
}

func TestSiteVisitNonMatchIsSilent(t *testing.T) {
	store := newTestStore(t)
	seedProjectWithSite(t, store, "수산리태양광", "전북 군산시 옥구읍 수산리")
	r := NewSiteVisitRecorder(store, testWikiLogger(), filepath.Join(t.TempDir(), "s.json"))

	// A place with no tracked 현장 records nothing — the privacy boundary.
	r.Record("서울특별시 강남구 역삼동")
	if _, err := store.ReadPage(wiki.LogPagePath("수산리태양광")); err == nil {
		t.Error("a non-matching place created a log page")
	}
	r.Record("") // empty — no-op, no panic
}

func TestSiteVisitFromPayload(t *testing.T) {
	store := newTestStore(t)
	seedProjectWithSite(t, store, "당진루프탑", "충남 당진시 송악읍")
	r := NewSiteVisitRecorder(store, testWikiLogger(), filepath.Join(t.TempDir(), "s.json"))

	r.RecordFromLocationPayload(`{"latitude":36.9,"longitude":126.8,"place":"충청남도 당진시 송악읍"}`)
	log, err := store.ReadPage(wiki.LogPagePath("당진루프탑"))
	if err != nil || !strings.Contains(log.Body, "방문 |") {
		t.Fatalf("payload visit not logged: err=%v body=%q", err, func() string {
			if log != nil {
				return log.Body
			}
			return ""
		}())
	}
	// A payload with no place field is a silent no-op (older client).
	r.RecordFromLocationPayload(`{"latitude":1,"longitude":2}`)
	// Malformed JSON is tolerated.
	r.RecordFromLocationPayload(`{bad`)
}

// TestMatchProjectSiteIsSitesOnly pins the false-positive guard: a project
// named after a place must NOT match a location by its NAME — only by an
// explicit 현장.
func TestMatchProjectSiteIsSitesOnly(t *testing.T) {
	store := newTestStore(t)
	// Project literally named "군산" but with a DIFFERENT site.
	seedProjectWithSite(t, store, "군산", "전남 신안군 비금면")
	// Another project whose site IS 수산리.
	seedProjectWithSite(t, store, "수산리건", "전북 군산시 옥구읍 수산리")

	// A 수산리 location must match the site-owning project, not "군산" by name.
	ref, key, ok := store.MatchProjectSite("전라북도 군산시 옥구읍 수산리")
	if !ok {
		t.Fatal("expected a site match")
	}
	if ref.Name != "수산리건" {
		t.Errorf("matched %q via key %q, want 수산리건 (sites-only)", ref.Name, key)
	}

	// A place matching no site returns false even if it shares a project name.
	if _, _, ok := store.MatchProjectSite("전북 군산시 시내"); ok {
		// "군산" project name must not cause a match — only its site 비금면 would.
		t.Error("matched by project name, not site")
	}
}

// TestMatchProjectSiteReturnsReadableCandidate pins the review fix: the matched
// string handed back is the human-readable site candidate (original spacing),
// not the normalized lowercase/letters-only key, so the 로그 line reads naturally.
func TestMatchProjectSiteReturnsReadableCandidate(t *testing.T) {
	store := newTestStore(t)
	seedProjectWithSite(t, store, "수산리태양광", "전북 군산시 옥구읍 수산리")

	_, cand, ok := store.MatchProjectSite("전라북도 군산시 옥구읍 수산리")
	if !ok {
		t.Fatal("expected a site match")
	}
	// The candidate must be the trailing admin unit as stored ("수산리"), never a
	// normalized key (which would strip case/spacing to a different form).
	if cand != "수산리" {
		t.Errorf("candidate = %q, want readable %q", cand, "수산리")
	}
}

func TestSanitizeLogText(t *testing.T) {
	cases := map[string]string{
		"전북 군산시 수산리":         "전북 군산시 수산리",
		"line1\nline2":       "line1 line2", // newline can't inject a new heading
		"a\r\n## 조작\t- b":    "a ## 조작 - b", // control chars collapsed, markdown inert as one token
		"  lots   of   ws  ": "lots of ws",  // runs of whitespace collapse
		"x\x00\x01y":         "x y",         // NUL/control bytes → space
	}
	for in, want := range cases {
		if got := sanitizeLogText(in); got != want {
			t.Errorf("sanitizeLogText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSiteVisitStatePersist(t *testing.T) {
	store := newTestStore(t)
	seedProjectWithSite(t, store, "수산리태양광", "전북 군산시 옥구읍 수산리")
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	r1 := NewSiteVisitRecorder(store, testWikiLogger(), statePath)
	r1.Record("전라북도 군산시 옥구읍 수산리")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state not persisted: %v", err)
	}

	// A fresh recorder loads the dedup state and does not double-log.
	r2 := NewSiteVisitRecorder(store, testWikiLogger(), statePath)
	log1, _ := store.ReadPage(wiki.LogPagePath("수산리태양광"))
	r2.Record("전라북도 군산시 옥구읍 수산리")
	log2, _ := store.ReadPage(wiki.LogPagePath("수산리태양광"))
	if len(log1.Body) != len(log2.Body) {
		t.Error("second recorder re-logged a visit already recorded today")
	}
}
