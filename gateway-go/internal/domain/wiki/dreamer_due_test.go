package wiki

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiaryMentionsPage(t *testing.T) {
	diary := "오늘 pl2-kia-epc-002 광주 캐노피 회신 정리"
	if !diaryMentionsPage(diary, "기아 광주 캐노피", "프로젝트/pl2-kia-epc-002/대표.md", "") {
		t.Fatal("folder name must count as a mention")
	}
	if !diaryMentionsPage(diary, "다른제목", "프로젝트/x/대표.md", "pl2-kia-epc-002") {
		t.Fatal("frozen code must count as a mention")
	}
	if diaryMentionsPage(diary, "당진 솔라빌리지", "프로젝트/당진-솔라빌리지/대표.md", "pl1-hre-epc-001") {
		t.Fatal("unrelated page must not count as mentioned")
	}
}

func TestCloseStaleDues_ClearsUnmentionedKeepsMentioned(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	oldDue := time.Now().AddDate(0, 0, -(staleDueCloseAfterDays + 3)).Format("2006-01-02")
	ghost := NewPage("유령 마감", "프로젝트", nil)
	ghost.Meta.Due = oldDue
	ghost.Body = "# 유령\n"
	if err := store.WritePage("프로젝트/ghost-deal/대표.md", ghost); err != nil {
		t.Fatal(err)
	}
	live := NewPage("기아 광주 캐노피", "프로젝트", nil)
	live.Meta.Due = oldDue
	live.Meta.Code = "pl2-kia-epc-002"
	live.Body = "# 광주\n"
	if err := store.WritePage("프로젝트/pl2-kia-epc-002/대표.md", live); err != nil {
		t.Fatal(err)
	}

	wd := &WikiDreamer{store: store}
	res := wd.closeStaleDues("오늘 pl2-kia-epc-002 회신 정리")
	if res.closed != 1 {
		t.Fatalf("closed=%d, want 1 (only the unmentioned ghost)", res.closed)
	}
	if len(res.alert) == 0 || !strings.Contains(res.alert[0], "유령 마감") {
		t.Fatalf("alert must lead with the closed ghost, got %v", res.alert)
	}

	got, err := store.ReadPage("프로젝트/ghost-deal/대표.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Due != "" {
		t.Fatalf("ghost due still set: %q", got.Meta.Due)
	}
	if !strings.Contains(got.Body, "기한 해제") {
		t.Fatalf("ghost body missing close note:\n%s", got.Body)
	}

	kept, err := store.ReadPage("프로젝트/pl2-kia-epc-002/대표.md")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Meta.Due != oldDue {
		t.Fatalf("mentioned page due cleared: %q", kept.Meta.Due)
	}
}

func TestCloseStaleDues_IgnoresFreshDue(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	fresh := NewPage("이번 주 마감", "프로젝트", nil)
	fresh.Meta.Due = time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	if err := store.WritePage("프로젝트/fresh/대표.md", fresh); err != nil {
		t.Fatal(err)
	}
	wd := &WikiDreamer{store: store}
	if res := wd.closeStaleDues(""); res.closed != 0 {
		t.Fatalf("fresh due closed=%d, want 0", res.closed)
	}
}
