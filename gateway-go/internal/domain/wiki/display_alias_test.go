package wiki

import (
	"path/filepath"
	"testing"
)

// A code-keyed folder renders with its rep title as the Korean alias; a
// descriptive folder whose name equals its title passes through unchanged.
func TestDisplayPathAnnotatesCodeFolders(t *testing.T) {
	store := testutilNewStore(t)

	rep := NewPage("비금도 154kV 해저케이블 (ZTT)", "프로젝트", nil)
	rep.Meta.Code = "nde-ztt-cbl-001"
	if err := store.WritePage("프로젝트/nde-ztt-cbl-001/대표.md", rep); err != nil {
		t.Fatal(err)
	}

	got := store.DisplayPath("프로젝트/nde-ztt-cbl-001/로그.md")
	want := "프로젝트/nde-ztt-cbl-001(비금도 154kV 해저케이블 (ZTT))/로그.md"
	if got != want {
		t.Errorf("DisplayPath = %q, want %q", got, want)
	}

	// Non-project path passes through.
	if got := store.DisplayPath("업무/BEP.md"); got != "업무/BEP.md" {
		t.Errorf("non-project path changed: %q", got)
	}
	// Unknown folder passes through.
	if got := store.DisplayPath("프로젝트/unknown-x/대표.md"); got != "프로젝트/unknown-x/대표.md" {
		t.Errorf("unknown folder changed: %q", got)
	}
}

// A sub-page deep inside a code folder resolves to its OWNING project's Korean
// name — the case the label form exists for, since such a page's own title is a
// mail subject or "진행 로그" and never names the project.
func TestProjectDisplayLabelResolvesOwningProject(t *testing.T) {
	store := testutilNewStore(t)
	rep := NewPage("기아 화성 국유지 태양광", "프로젝트", nil)
	if err := store.WritePage("프로젝트/pl2-kia-epc-001/대표.md", rep); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"프로젝트/pl2-kia-epc-001/대표.md",
		"프로젝트/pl2-kia-epc-001/로그.md",
		"프로젝트/pl2-kia-epc-001/메일분석/abc@topsolar.kr.md",
	} {
		if got := store.ProjectDisplayLabel(path); got != "기아 화성 국유지 태양광" {
			t.Errorf("ProjectDisplayLabel(%q) = %q, want 기아 화성 국유지 태양광", path, got)
		}
	}

	// Non-project and unknown paths yield no label, so callers omit the field
	// rather than rendering an empty "프로젝트: " row.
	if got := store.ProjectDisplayLabel("업무/BEP.md"); got != "" {
		t.Errorf("non-project label = %q, want empty", got)
	}
	if got := store.ProjectDisplayLabel("프로젝트/unknown-x/로그.md"); got != "" {
		t.Errorf("unknown-project label = %q, want empty", got)
	}
}

// The alias cache must refresh when pages change (generation-keyed).
func TestProjectDisplayAliasRefreshesOnWrite(t *testing.T) {
	store := testutilNewStore(t)
	rep := NewPage("옛 제목", "프로젝트", nil)
	if err := store.WritePage("프로젝트/pl2-tst-epc-001/대표.md", rep); err != nil {
		t.Fatal(err)
	}
	if got := store.ProjectDisplayAlias("pl2-tst-epc-001"); got != "옛 제목" {
		t.Fatalf("alias = %q, want 옛 제목", got)
	}
	rep2 := NewPage("새 제목", "프로젝트", nil)
	if err := store.WritePage("프로젝트/pl2-tst-epc-001/대표.md", rep2); err != nil {
		t.Fatal(err)
	}
	if got := store.ProjectDisplayAlias("pl2-tst-epc-001"); got != "새 제목" {
		t.Errorf("alias after rewrite = %q, want 새 제목", got)
	}
}

func testutilNewStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "wiki"), "")
	if err != nil {
		t.Fatal(err)
	}
	return store
}
