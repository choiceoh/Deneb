package wikiwork

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestRecordMeetingAttendanceByPathReturnsHandledForAllInputs(t *testing.T) {
	store := newTestStore(t)
	rep := &wiki.Page{Meta: wiki.Frontmatter{Title: "기아PE", Category: "프로젝트"}, Body: "## 현재 상태\n"}
	if err := store.WritePage(wiki.RepPagePath("기아PE"), rep); err != nil {
		t.Fatal(err)
	}

	// A project rep path logs a 회의 op and reports handled.
	if !RecordMeetingAttendanceByPath(store, wiki.RepPagePath("기아PE"), "기아PE 발주 협의", "2026-07-13") {
		t.Fatal("expected attendance handled")
	}
	log, err := store.ReadPage(wiki.LogPagePath("기아PE"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(log.Body, "## [2026-07-13] 회의 | 기아PE 발주 협의 — 참석") {
		t.Errorf("attendance op missing: %q", log.Body)
	}

	// A non-project path is HANDLED (deliberate skip — the typed caller only ever
	// passes real project reps) and writes nothing to retry.
	if !RecordMeetingAttendanceByPath(store, "존재하지않는거래처", "회의", "2026-07-13") {
		t.Error("non-project path should report handled")
	}
	// nil store / empty path / empty date all report handled (nothing to retry).
	if !RecordMeetingAttendanceByPath(nil, wiki.RepPagePath("기아PE"), "회의", "2026-07-13") {
		t.Error("nil store must report handled")
	}
	if !RecordMeetingAttendanceByPath(store, "", "회의", "2026-07-13") {
		t.Error("empty path must report handled")
	}
	if !RecordMeetingAttendanceByPath(store, wiki.RepPagePath("기아PE"), "회의", "") {
		t.Error("empty date must report handled")
	}
}
