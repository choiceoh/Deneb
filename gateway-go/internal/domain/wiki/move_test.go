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
