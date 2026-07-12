package wikiwork

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestRecordMeetingAttendance(t *testing.T) {
	store := newTestStore(t)
	rep := &wiki.Page{Meta: wiki.Frontmatter{Title: "기아PE", Category: "프로젝트"}, Body: "## 현재 상태\n"}
	if err := store.WritePage(wiki.RepPagePath("기아PE"), rep); err != nil {
		t.Fatal(err)
	}

	// A project match logs a 회의 op.
	if !RecordMeetingAttendance(store, "기아PE", "기아PE 발주 협의", "2026-07-13") {
		t.Fatal("expected attendance recorded")
	}
	log, err := store.ReadPage(wiki.LogPagePath("기아PE"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(log.Body, "## [2026-07-13] 회의 | 기아PE 발주 협의 — 참석") {
		t.Errorf("attendance op missing: %q", log.Body)
	}

	// An unknown target (no such project — e.g. a counterparty-only match) is a
	// silent no-op.
	if RecordMeetingAttendance(store, "존재하지않는거래처", "회의", "2026-07-13") {
		t.Error("recorded attendance for a non-project target")
	}
	// Empty inputs no-op.
	if RecordMeetingAttendance(nil, "기아PE", "회의", "2026-07-13") {
		t.Error("nil store must no-op")
	}
	if RecordMeetingAttendance(store, "", "회의", "2026-07-13") {
		t.Error("empty target must no-op")
	}
}
