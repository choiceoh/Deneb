package wiki

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newProjectTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
}

// TestKnownProjects_ListsRepPagesFolderFormWinsRejectsRawAndSubPages: a project is
// its 대표페이지 — the in-folder 프로젝트/<name>/대표.md form or the legacy flat
// 프로젝트/<name>.md form (folder form wins when both exist); raw-data buckets
// (메일분석/, mail-analyses/, 거래/), sub-pages, and other categories are excluded.
func TestKnownProjects_ListsRepPagesFolderFormWinsRejectsRawAndSubPages(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	for _, p := range []struct{ path, title, cat string }{
		{"프로젝트/영산고.md", "영산고", "프로젝트"},              // legacy flat 대표페이지
		{"프로젝트/남도풍력/대표.md", "남도풍력", "프로젝트"},         // in-folder 대표페이지
		{"프로젝트/남도풍력/로그.md", "남도풍력 진행 로그", "프로젝트"},   // sub-page → excluded
		{"프로젝트/부산8호.md", "부산8호", "프로젝트"},            // legacy flat…
		{"프로젝트/부산8호/대표.md", "부산8호", "프로젝트"},         // …and in-folder → folder form wins
		{"프로젝트/mail-analyses/abc.md", "메일", "프로젝트"}, // raw data → excluded
		{"프로젝트/메일분석/def.md", "메일", "프로젝트"},          // raw data → excluded
		{"프로젝트/남도풍력/메일분석/ghi.md", "메일", "프로젝트"},     // per-project raw data → excluded
		{"프로젝트/거래/탑솔라.md", "탑솔라", "프로젝트"},           // raw data → excluded
		{"인물/김민준.md", "김민준", "인물"},                  // not a project
	} {
		page := NewPage(p.title, p.cat, nil)
		page.Body = "# " + p.title
		if err := store.WritePage(p.path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", p.path, err)
		}
	}

	got := store.knownProjects()
	if len(got) != 3 {
		t.Fatalf("knownProjects() = %d entries (%+v), want 3", len(got), got)
	}
	// Sorted by name.
	if got[0].Name != "남도풍력" || got[0].Path != "프로젝트/남도풍력/대표.md" {
		t.Errorf("got[0] = %+v, want 남도풍력 / 프로젝트/남도풍력/대표.md", got[0])
	}
	if got[1].Name != "부산8호" || got[1].Path != "프로젝트/부산8호/대표.md" {
		t.Errorf("got[1] = %+v, want 부산8호 folder form to win over the legacy flat page", got[1])
	}
	if got[2].Name != "영산고" || got[2].Path != "프로젝트/영산고.md" {
		t.Errorf("got[2] = %+v, want 영산고 / 프로젝트/영산고.md", got[2])
	}
}

// TestEnsureProjectPage_TitlesNewRepPageWithProjectNameNotLiteralDefault: a missing
// in-folder 대표페이지 is minted with the project's name, not the literal "대표".
func TestEnsureProjectPage_TitlesNewRepPageWithProjectNameNotLiteralDefault(t *testing.T) {
	page := ensureProjectPage(nil, "프로젝트/영산고/대표.md")
	if page.Meta.Title != "영산고" {
		t.Errorf("Title = %q, want 영산고", page.Meta.Title)
	}
	legacy := ensureProjectPage(nil, "프로젝트/부산8호.md")
	if legacy.Meta.Title != "부산8호" {
		t.Errorf("legacy Title = %q, want 부산8호", legacy.Meta.Title)
	}
}

// TestSetProjectStatus_RoundTrip: setProjectStatus writes the 현재 상태 section
// (creating the page), and ProjectStatuses reads it back with due + updated.
func TestSetProjectStatus_RoundTrip(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	lines := []string{"모듈 발주 완료", "계약 체결", "납기 6월 말"}
	if err := store.setProjectStatus("프로젝트/영산고.md", lines, "2026-06-30", now, ""); err != nil {
		t.Fatalf("setProjectStatus: %v", err)
	}

	statuses, err := store.ProjectStatuses()
	if err != nil {
		t.Fatalf("ProjectStatuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	st := statuses[0]
	if st.Name != "영산고" || st.Path != "프로젝트/영산고.md" {
		t.Errorf("status identity = %+v", st)
	}
	if st.Due != "2026-06-30" {
		t.Errorf("due = %q, want 2026-06-30", st.Due)
	}
	if st.UpdatedMs != dateToMillis("2026-06-23") {
		t.Errorf("updatedMs = %d, want %d", st.UpdatedMs, dateToMillis("2026-06-23"))
	}
	if len(st.Bullets) != 3 || st.Bullets[0] != "모듈 발주 완료" {
		t.Errorf("bullets = %v, want the 3 lines in order", st.Bullets)
	}

	// setProjectStatus replaces (not appends): a second call leaves only the new lines.
	if err := store.setProjectStatus("프로젝트/영산고.md", []string{"시운전 시작"}, "", now, ""); err != nil {
		t.Fatalf("setProjectStatus #2: %v", err)
	}
	statuses, _ = store.ProjectStatuses()
	if len(statuses) != 1 || len(statuses[0].Bullets) != 1 || statuses[0].Bullets[0] != "시운전 시작" {
		t.Fatalf("after replace, bullets = %v, want [시운전 시작]", statuses[0].Bullets)
	}
}

// TestAppendProjectStatusLine_PrependsIdempotentByRefAndCapped: prepend
// newest-first, strip the provenance marker on read, idempotent by ref, and
// capped.
func TestAppendProjectStatusLine_PrependsIdempotentByRefAndCapped(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()
	path := "프로젝트/영산고.md"
	day := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)

	if err := store.AppendProjectStatusLine(path, "탑솔라 견적서 수신", "mail:m1", day); err != nil {
		t.Fatalf("append m1: %v", err)
	}
	if err := store.AppendProjectStatusLine(path, "계약서 회신", "mail:m2", day); err != nil {
		t.Fatalf("append m2: %v", err)
	}
	// Idempotent: re-appending the same ref is a no-op.
	if err := store.AppendProjectStatusLine(path, "탑솔라 견적서 수신", "mail:m1", day); err != nil {
		t.Fatalf("append m1 again: %v", err)
	}

	statuses, err := store.ProjectStatuses()
	if err != nil {
		t.Fatalf("ProjectStatuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	b := statuses[0].Bullets
	if len(b) != 2 {
		t.Fatalf("bullets = %v, want 2 (idempotent dedupe)", b)
	}
	// Newest first; marker stripped; date prefix present.
	if b[0] != "6월 23일 계약서 회신" {
		t.Errorf("bullets[0] = %q, want dated newest-first line without marker", b[0])
	}

	// Cap: push well past the cap; only the most recent maxProjectStatusBullets survive.
	for i := 0; i < maxProjectStatusBullets+5; i++ {
		ref := "mail:bulk" + string(rune('a'+i))
		if err := store.AppendProjectStatusLine(path, "활동 "+string(rune('a'+i)), ref, day); err != nil {
			t.Fatalf("bulk append: %v", err)
		}
	}
	statuses, _ = store.ProjectStatuses()
	if len(statuses[0].Bullets) != maxProjectStatusBullets {
		t.Fatalf("bullets after bulk = %d, want capped at %d", len(statuses[0].Bullets), maxProjectStatusBullets)
	}
}

// TestAppendProjectStatusLineAt_EventDate: an event date (e.g. a deal document's
// own date) on a different day than capture leads the bullet with the event date
// and notes the capture day; same-day / empty / invalid event dates render
// capture-dated as before.
func TestAppendProjectStatusLineAt_EventDate(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()
	path := "프로젝트/영산고.md"
	capture := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)

	if err := store.AppendProjectStatusLineAt(path, "견적서 · 탑솔라 수신", "2026-08-01", "mail:m1", capture); err != nil {
		t.Fatalf("append event-dated: %v", err)
	}
	if err := store.AppendProjectStatusLineAt(path, "계약서 회신", "2026-08-03", "mail:m2", capture); err != nil {
		t.Fatalf("append same-day: %v", err)
	}
	if err := store.AppendProjectStatusLineAt(path, "전화 통화", "", "mail:m3", capture); err != nil {
		t.Fatalf("append empty: %v", err)
	}

	statuses, err := store.ProjectStatuses()
	if err != nil || len(statuses) != 1 {
		t.Fatalf("ProjectStatuses: %v (n=%d)", err, len(statuses))
	}
	b := statuses[0].Bullets // newest first: m3, m2, m1
	if b[0] != "8월 3일 전화 통화" {
		t.Errorf("empty event date bullet = %q, want capture-dated, no note", b[0])
	}
	if b[1] != "8월 3일 계약서 회신" {
		t.Errorf("same-day event bullet = %q, want capture-dated, no note", b[1])
	}
	if b[2] != "8월 1일 견적서 · 탑솔라 수신 (8/3 기록)" {
		t.Errorf("differing event bullet = %q, want event-dated + capture note", b[2])
	}
}

func TestStatusBulletDates(t *testing.T) {
	capture := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	if lead, note := statusBulletDates("2026-08-01", capture); lead != "8월 1일" || note != " (8/3 기록)" {
		t.Errorf("differing day: lead=%q note=%q", lead, note)
	}
	if lead, note := statusBulletDates("2026-08-03", capture); lead != "8월 3일" || note != "" {
		t.Errorf("same day: lead=%q note=%q", lead, note)
	}
	for _, bad := range []string{"", "not-a-date", "8월 1일", "2026/08/01"} {
		if lead, note := statusBulletDates(bad, capture); lead != "8월 3일" || note != "" {
			t.Errorf("invalid %q: lead=%q note=%q (want capture-dated, no note)", bad, lead, note)
		}
	}
}
