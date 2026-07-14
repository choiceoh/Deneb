package wiki

import "testing"

func newMoveStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestMovePage(t *testing.T) {
	s := newMoveStore(t)
	page := NewPage("탑솔라", "프로젝트", nil)
	page.Body = "# 탑솔라\n본문 보존 확인"
	if err := s.WritePage("프로젝트/탑솔라.md", page); err != nil {
		t.Fatal(err)
	}

	if err := s.MovePage("프로젝트/탑솔라.md", "인물/탑솔라.md"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	if p, _ := s.ReadPage("프로젝트/탑솔라.md"); p != nil {
		t.Error("source still present after move")
	}
	moved, err := s.ReadPage("인물/탑솔라.md")
	if err != nil || moved == nil {
		t.Fatalf("read moved page: %v", err)
	}
	if moved.Meta.Category != "인물" {
		t.Errorf("category = %q, want 인물 (frontmatter follows the new directory)", moved.Meta.Category)
	}
	if moved.Body != "# 탑솔라\n본문 보존 확인" {
		t.Errorf("body not preserved through move: %q", moved.Body)
	}
}

func TestMovePage_RejectsExistingTarget(t *testing.T) {
	s := newMoveStore(t)
	if err := s.WritePage("프로젝트/a.md", NewPage("A", "프로젝트", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePage("인물/a.md", NewPage("B", "인물", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.MovePage("프로젝트/a.md", "인물/a.md"); err == nil {
		t.Error("expected error moving onto an existing target (no overwrite)")
	}
	// Neither side is clobbered — the move is refused, not destructive.
	if p, _ := s.ReadPage("프로젝트/a.md"); p == nil {
		t.Error("source was lost on a refused move")
	}
	if p, _ := s.ReadPage("인물/a.md"); p == nil {
		t.Error("target was overwritten on a refused move")
	}
}

// TestMovePage_RepointsInboundReferences: pages whose Related pointed at the
// old path follow the move — the graph edge survives instead of dangling.
func TestMovePage_RepointsInboundReferences(t *testing.T) {
	s := newMoveStore(t)
	target := NewPage("탑솔라", "프로젝트", nil)
	if err := s.WritePage("프로젝트/탑솔라.md", target); err != nil {
		t.Fatal(err)
	}
	referrer := NewPage("견적 메일", "프로젝트", nil)
	referrer.Meta.Related = []string{"프로젝트/탑솔라.md"}
	if err := s.WritePage("프로젝트/메일분석/abc.md", referrer); err != nil {
		t.Fatal(err)
	}

	if err := s.MovePage("프로젝트/탑솔라.md", "프로젝트/탑솔라/대표.md"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	got, err := s.ReadPage("프로젝트/메일분석/abc.md")
	if err != nil {
		t.Fatalf("read referrer: %v", err)
	}
	var hasNew, hasOld bool
	for _, r := range got.Meta.Related {
		if r == "프로젝트/탑솔라/대표.md" {
			hasNew = true
		}
		if r == "프로젝트/탑솔라.md" {
			hasOld = true
		}
	}
	if !hasNew || hasOld {
		t.Errorf("related = %v, want old path repointed to the new one", got.Meta.Related)
	}
}

// A repoint is metadata hygiene on the referencing page, not activity: it must
// not refresh Updated (dormancy detection and counterparty activity windows key
// off it — a batch move would otherwise make every referrer look freshly
// active). Same doctrine as addBacklink/removeBacklink/PruneDeadRelatedLinks.
func TestMovePage_RepointDoesNotStampReferrerUpdated(t *testing.T) {
	s := newMoveStore(t)
	if err := s.WritePage("프로젝트/탑솔라.md", NewPage("탑솔라", "프로젝트", nil)); err != nil {
		t.Fatal(err)
	}
	referrer := NewPage("휴면 프로젝트", "프로젝트", nil)
	referrer.Meta.Updated = "2025-01-01" // dormant long before the move
	referrer.Meta.Related = []string{"프로젝트/탑솔라.md"}
	if err := s.WritePage("프로젝트/휴면/대표.md", referrer); err != nil {
		t.Fatal(err)
	}

	if err := s.MovePage("프로젝트/탑솔라.md", "프로젝트/탑솔라/대표.md"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	got, err := s.ReadPage("프로젝트/휴면/대표.md")
	if err != nil {
		t.Fatalf("read referrer: %v", err)
	}
	repointed := false
	for _, r := range got.Meta.Related {
		if r == "프로젝트/탑솔라/대표.md" {
			repointed = true
		}
	}
	if !repointed {
		t.Fatalf("related = %v, want the moved path repointed", got.Meta.Related)
	}
	if got.Meta.Updated != "2025-01-01" {
		t.Errorf("Updated = %q, want 2025-01-01 (repoint must not stamp)", got.Meta.Updated)
	}
	// The master index mirrors the stamp — FlagDormantProjects reads it there.
	if e, ok := s.snapshotEntries()["프로젝트/휴면/대표.md"]; !ok || e.Updated != "2025-01-01" {
		t.Errorf("index Updated = %q (present=%v), want 2025-01-01", e.Updated, ok)
	}
}

func TestMovePage_SourceNotFound(t *testing.T) {
	s := newMoveStore(t)
	if err := s.MovePage("프로젝트/missing.md", "인물/missing.md"); err == nil {
		t.Error("expected error moving a nonexistent source")
	}
}

func TestMovePage_NoopSamePath(t *testing.T) {
	s := newMoveStore(t)
	if err := s.WritePage("프로젝트/a.md", NewPage("A", "프로젝트", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.MovePage("프로젝트/a.md", "프로젝트/a.md"); err != nil {
		t.Errorf("same-path move should be a no-op, got %v", err)
	}
	if p, _ := s.ReadPage("프로젝트/a.md"); p == nil {
		t.Error("page vanished on a no-op move")
	}
}
