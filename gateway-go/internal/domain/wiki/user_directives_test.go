package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUserDirectivesSection(t *testing.T) {
	if got := renderUserDirectivesSection(nil); got != "" {
		t.Fatalf("empty input = %q, want empty", got)
	}
	ds := []userDirective{
		{title: "톤", summary: "간결한 한국어", importance: 0.5, updated: "2026-06-01"},
		{title: "알림", summary: "과도한 알림 금지", importance: 0.9, updated: "2026-05-01"},
		{title: "이름만"},
	}
	got := renderUserDirectivesSection(ds)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 || lines[0] != userDirectivesHeading {
		t.Fatalf("layout wrong (%d lines): %q", len(lines), got)
	}
	// importance desc → 알림(0.9) first, then 톤(0.5), then title-only.
	if lines[1] != "- 알림: 과도한 알림 금지" {
		t.Errorf("line1 = %q", lines[1])
	}
	if lines[2] != "- 톤: 간결한 한국어" {
		t.Errorf("line2 = %q", lines[2])
	}
	if lines[3] != "- 이름만" {
		t.Errorf("line3 = %q", lines[3])
	}
}

func TestRenderUserDirectives_Cap(t *testing.T) {
	var ds []userDirective
	for i := 0; i < maxUserDirectives+5; i++ {
		ds = append(ds, userDirective{title: fmt.Sprintf("d%02d", i), importance: float64(i)})
	}
	got := renderUserDirectivesSection(ds)
	if n := strings.Count(got, "\n- "); n != maxUserDirectives {
		t.Fatalf("bullets = %d, want %d", n, maxUserDirectives)
	}
	// The highest-importance entry must survive the cap.
	if !strings.Contains(got, fmt.Sprintf("d%02d", maxUserDirectives+4)) {
		t.Errorf("top-importance directive dropped: %q", got)
	}
}

func TestMergeUserDirectives(t *testing.T) {
	section := renderUserDirectivesSection([]userDirective{{title: "톤", summary: "간결"}})

	existing := "# 사용자\n\n이름: 홍길동\n"
	merged := mergeUserDirectives(existing, section)
	if !strings.Contains(merged, "이름: 홍길동") {
		t.Fatalf("lost user content: %q", merged)
	}
	if !strings.Contains(merged, userDirectivesBegin) || !strings.Contains(merged, userDirectivesEnd) {
		t.Fatalf("missing markers: %q", merged)
	}
	if !strings.Contains(merged, "- 톤: 간결") {
		t.Fatalf("missing directive: %q", merged)
	}

	// Idempotent: re-merging the same section is byte-identical.
	if again := mergeUserDirectives(merged, section); again != merged {
		t.Fatalf("not idempotent:\n%q\nvs\n%q", merged, again)
	}

	// Replace with a different section; user content preserved, old directive gone.
	section2 := renderUserDirectivesSection([]userDirective{{title: "알림", summary: "금지"}})
	replaced := mergeUserDirectives(merged, section2)
	if strings.Contains(replaced, "- 톤: 간결") {
		t.Fatalf("old directive survived: %q", replaced)
	}
	if !strings.Contains(replaced, "- 알림: 금지") || !strings.Contains(replaced, "이름: 홍길동") {
		t.Fatalf("replace lost content: %q", replaced)
	}

	// Remove (empty section) drops the block but keeps user content, and is a no-op afterward.
	removed := mergeUserDirectives(replaced, "")
	if strings.Contains(removed, userDirectivesBegin) || strings.Contains(removed, "알림") {
		t.Fatalf("block not removed: %q", removed)
	}
	if !strings.Contains(removed, "이름: 홍길동") {
		t.Fatalf("remove lost user content: %q", removed)
	}
	if again := mergeUserDirectives(removed, ""); again != removed {
		t.Fatalf("empty merge not a no-op: %q vs %q", removed, again)
	}

	// Empty existing file + section.
	fresh := mergeUserDirectives("", section)
	if !strings.Contains(fresh, userDirectivesBegin) || !strings.Contains(fresh, "- 톤: 간결") {
		t.Fatalf("fresh merge wrong: %q", fresh)
	}
}

func TestCollapseSpaces(t *testing.T) {
	if got := collapseSpaces("a\n  b\t c"); got != "a b c" {
		t.Fatalf("collapseSpaces = %q", got)
	}
}

func TestDistillUserDirectives(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	ws := t.TempDir()

	write := func(rel, title, summary string, imp float64, mut func(*Frontmatter)) {
		p := &Page{
			Meta: Frontmatter{Title: title, Summary: summary, Category: userPrefCategory, Importance: imp},
			Body: "# " + title + "\n",
		}
		if mut != nil {
			mut(&p.Meta)
		}
		if werr := store.WritePage(rel, p); werr != nil {
			t.Fatal(werr)
		}
	}
	write("사용자/톤.md", "톤 선호", "간결한 한국어, 서두 금지", 0.8, nil)
	write("사용자/알림.md", "알림", "과도한 알림 금지", 0.6, nil)
	write("사용자/old.md", "옛 규칙", "대체됨", 0.9, func(fm *Frontmatter) { fm.SupersededBy = "사용자/톤.md" })
	write("사용자/arch.md", "보관됨", "보관", 0.9, func(fm *Frontmatter) { fm.Archived = true })

	wd := &WikiDreamer{store: store, workspaceDir: ws}
	n, err := wd.distillUserDirectives()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("directives = %d, want 2 (active only)", n)
	}

	data, err := os.ReadFile(filepath.Join(ws, userFileName))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "톤 선호: 간결한 한국어, 서두 금지") || !strings.Contains(got, "알림: 과도한 알림 금지") {
		t.Fatalf("active directives missing: %q", got)
	}
	if strings.Contains(got, "옛 규칙") || strings.Contains(got, "보관됨") {
		t.Fatalf("superseded/archived leaked into directives: %q", got)
	}

	// Re-running with no 사용자 change must not rewrite USER.md (byte-stable).
	before, _ := os.ReadFile(filepath.Join(ws, userFileName))
	if _, err := wd.distillUserDirectives(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(ws, userFileName))
	if string(before) != string(after) {
		t.Fatalf("second run mutated USER.md (not byte-stable)")
	}
}
