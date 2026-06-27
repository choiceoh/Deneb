package wiki

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLifecycleStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestFrontmatterStatusRoundTrip(t *testing.T) {
	p := NewPage("탑솔라 ESS", projectCategoryPrefix, nil)
	p.Meta.Status = ProjectStatusDone
	p.Body = "# 탑솔라 ESS\n\n## 현재 상태\n\n- 준공 완료\n"

	parsed, err := ParsePage(p.Render())
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if parsed.Meta.Status != ProjectStatusDone {
		t.Fatalf("status did not round-trip: got %q want %q", parsed.Meta.Status, ProjectStatusDone)
	}

	// An empty status must not emit a `status:` frontmatter line.
	p.Meta.Status = ""
	if strings.Contains(string(p.Render()), "status:") {
		t.Fatalf("empty status should not render a status: line")
	}
}

func TestProjectStatusesActiveFirst_RankBeatsRecency(t *testing.T) {
	s := newLifecycleStore(t)

	// Seed a 현재 상태 section (required for ProjectStatuses to include a project)
	// then set lifecycle status, both stamped with a controlled date. The active
	// project is the OLDEST and the completed one the NEWEST, so ordering by
	// recency alone would float 완료 to the top — rank must override that.
	seed := func(path, title, status, date string) {
		if err := s.SetProjectStatus(path, []string{"메모 " + title}, "", day(date)); err != nil {
			t.Fatalf("SetProjectStatus %s: %v", path, err)
		}
		if err := s.SetProjectLifecycleStatus(path, status, day(date)); err != nil {
			t.Fatalf("SetProjectLifecycleStatus %s: %v", path, err)
		}
	}
	seed("프로젝트/한빛풍력.md", "한빛 풍력", ProjectStatusActive, "2026-01-01")   // active, oldest
	seed("프로젝트/영암ESS.md", "영암 ESS", ProjectStatusOnHold, "2026-03-01") // on hold, middle
	seed("프로젝트/탑솔라.md", "탑솔라 ESS", ProjectStatusDone, "2026-06-01")    // done, newest

	statuses, err := s.ProjectStatuses()
	if err != nil {
		t.Fatalf("ProjectStatuses: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 projects, got %d (%+v)", len(statuses), statuses)
	}
	gotOrder := []string{statuses[0].Status, statuses[1].Status, statuses[2].Status}
	want := []string{ProjectStatusActive, ProjectStatusOnHold, ProjectStatusDone}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("rank ordering wrong: got %v want %v", gotOrder, want)
		}
	}
}

func TestProjectStatusesActiveFirst_RecencyWithinRank(t *testing.T) {
	s := newLifecycleStore(t)
	seed := func(path, title, date string) {
		if err := s.SetProjectStatus(path, []string{"메모 " + title}, "", day(date)); err != nil {
			t.Fatalf("SetProjectStatus %s: %v", path, err)
		}
		// Leave status empty (active default) for both.
	}
	seed("프로젝트/old.md", "오래된 프로젝트", "2026-01-01")
	seed("프로젝트/new.md", "최근 프로젝트", "2026-05-01")

	statuses, err := s.ProjectStatuses()
	if err != nil {
		t.Fatalf("ProjectStatuses: %v", err)
	}
	// Both are active (empty status); within a rank the newer-updated leads.
	if len(statuses) != 2 || statuses[0].Path != "프로젝트/new.md" {
		t.Fatalf("within active rank, newest should lead: %+v", statuses)
	}
}

func TestSetProjectLifecycleStatus_SetsAndDecouplesFromArchive(t *testing.T) {
	s := newLifecycleStore(t)
	path := "프로젝트/탑솔라.md"
	if err := s.SetProjectStatus(path, []string{"진행 메모"}, "", day("2026-02-01")); err != nil {
		t.Fatalf("SetProjectStatus: %v", err)
	}
	if err := s.SetProjectLifecycleStatus(path, ProjectStatusDone, day("2026-06-01")); err != nil {
		t.Fatalf("SetProjectLifecycleStatus: %v", err)
	}

	page, err := s.ReadPage(path)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Meta.Status != ProjectStatusDone {
		t.Fatalf("status not set: %q", page.Meta.Status)
	}
	// 완료 must NOT archive — facts stay findable; status is foregrounding only.
	if page.Meta.Archived {
		t.Fatalf("completing a project must not archive it (facts stay findable)")
	}
}

func TestSetProjectLifecycleStatus_RejectsUnknownValue(t *testing.T) {
	s := newLifecycleStore(t)
	path := "프로젝트/탑솔라.md"
	if err := s.SetProjectStatus(path, []string{"메모"}, "", day("2026-02-01")); err != nil {
		t.Fatalf("SetProjectStatus: %v", err)
	}
	if err := s.SetProjectLifecycleStatus(path, "끝남", day("2026-06-01")); err == nil {
		t.Fatalf("expected an error for an unknown status value")
	}
	// Empty is valid (clears back to active default).
	if err := s.SetProjectLifecycleStatus(path, "", day("2026-06-01")); err != nil {
		t.Fatalf("empty status should be valid: %v", err)
	}
}

func TestDreamRollupPreservesLifecycleStatus(t *testing.T) {
	s := newLifecycleStore(t)
	path := "프로젝트/탑솔라.md"
	if err := s.SetProjectStatus(path, []string{"초기 메모"}, "", day("2026-02-01")); err != nil {
		t.Fatalf("SetProjectStatus: %v", err)
	}
	if err := s.SetProjectLifecycleStatus(path, ProjectStatusDone, day("2026-03-01")); err != nil {
		t.Fatalf("SetProjectLifecycleStatus: %v", err)
	}
	// A later dream-cycle roll-up rewrites the 현재 상태 section — it must NOT clobber
	// the operator-set lifecycle status (the dreamer preserves, never sets it).
	if err := s.SetProjectStatus(path, []string{"갱신된 롤업"}, "", day("2026-04-01")); err != nil {
		t.Fatalf("SetProjectStatus rollup: %v", err)
	}

	page, err := s.ReadPage(path)
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Meta.Status != ProjectStatusDone {
		t.Fatalf("dream roll-up clobbered lifecycle status: got %q want %q", page.Meta.Status, ProjectStatusDone)
	}
}
