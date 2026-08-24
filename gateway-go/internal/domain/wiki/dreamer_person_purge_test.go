package wiki

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubBody is what mention-seeding + contacts sync leave behind: template
// sections and synced contact lines, no prose.
const purgeStubBody = `## 소속 · 직책

- **소속**: 탑솔라(주)

## 담당 · 관계

_(미기재)_

## 연락처

- 전화: 010-0000-0000

_주소록에서 동기화됨_
`

func newPurgeTestDreamer(t *testing.T) (*WikiDreamer, *Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &WikiDreamer{store: store, logger: slog.Default()}, store
}

func writePurgePerson(t *testing.T, store *Store, name, created, body string, pid string) {
	t.Helper()
	page := &Page{
		Meta: Frontmatter{ID: name, Title: name, Category: "인물", Created: created, PID: pid},
		Body: body,
	}
	if err := store.WritePage("인물/"+name+".md", page); err != nil {
		t.Fatal(err)
	}
}

// The operator's order encoded: a person page with no content past the grace
// window is deleted; content, org-chart identity (pid), or youth protects it.
func TestPurgeDreamPersonStubsDeletesOnlyOldContentlessPages(t *testing.T) {
	wd, store := newPurgeTestDreamer(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	old := "2026-07-01"
	young := now.AddDate(0, 0, -3).Format("2006-01-02")

	writePurgePerson(t, store, "옛스텁", old, purgeStubBody, "")
	writePurgePerson(t, store, "신규시드", young, purgeStubBody, "")
	writePurgePerson(t, store, "조직도인물", old, purgeStubBody, "p-ts-001")
	writePurgePerson(t, store, "산문보유", old, "금호타이어 곡성 담당, 계약 창구. 실사 일정을 조율한다.\n"+purgeStubBody, "")
	// No Created at all: predates the date convention — old by definition.
	writePurgePerson(t, store, "무일자스텁", "", purgeStubBody, "")

	purged := wd.purgeDreamPersonStubs(now)
	if purged != 2 {
		t.Fatalf("purged = %d, want 2 (옛스텁+무일자스텁)", purged)
	}
	for name, wantGone := range map[string]bool{
		"옛스텁": true, "무일자스텁": true,
		"신규시드": false, "조직도인물": false, "산문보유": false,
	} {
		_, err := store.ReadPage("인물/" + name + ".md")
		gone := err != nil
		if gone != wantGone {
			t.Errorf("%s: gone=%v, want %v (err=%v)", name, gone, wantGone, err)
		}
	}
}

// Deleting through the store must also clear the purged page out of surviving
// Related lists — that is what finally removes the legacy namesake edges the
// enrichment guard could only stop from growing.
func TestPurgeDreamPersonStubsCleansSurvivorRelatedLinks(t *testing.T) {
	wd, store := newPurgeTestDreamer(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	writePurgePerson(t, store, "강동민", "2026-07-01", purgeStubBody, "")
	survivor := &Page{
		Meta: Frontmatter{
			ID: "강민수", Title: "강민수", Category: "인물", Created: "2026-07-01",
			Related: []string{"인물/강동민.md"},
		},
		Body: "탑솔라 구매 담당. 케이블 발주 창구.",
	}
	if err := store.WritePage("인물/강민수.md", survivor); err != nil {
		t.Fatal(err)
	}

	if purged := wd.purgeDreamPersonStubs(now); purged != 1 {
		t.Fatalf("purged = %d, want 1", purged)
	}
	got, err := store.ReadPage("인물/강민수.md")
	if err != nil {
		t.Fatalf("survivor unreadable: %v", err)
	}
	for _, r := range got.Meta.Related {
		if strings.Contains(r, "강동민") {
			t.Fatalf("purged page still referenced from survivor Related: %v", got.Meta.Related)
		}
	}
}

func TestPurgeDreamPersonStubsRespectsPerCycleBound(t *testing.T) {
	wd, store := newPurgeTestDreamer(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	total := personStubPurgeMaxPerCycle + 7
	for i := 0; i < total; i++ {
		writePurgePerson(t, store, "스텁"+strconvItoa(i), "2026-07-01", purgeStubBody, "")
	}
	if purged := wd.purgeDreamPersonStubs(now); purged != personStubPurgeMaxPerCycle {
		t.Fatalf("purged = %d, want the per-cycle bound %d", purged, personStubPurgeMaxPerCycle)
	}
	// The next cycle drains the rest.
	if purged := wd.purgeDreamPersonStubs(now); purged != 7 {
		t.Fatalf("second cycle purged = %d, want 7", purged)
	}
	entries, err := os.ReadDir(filepath.Join(store.dir, "인물"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("expected an empty 인물 dir, %d entries left", len(entries))
	}
}

func strconvItoa(i int) string { return string(rune('a'+i/26)) + string(rune('a'+i%26)) }
