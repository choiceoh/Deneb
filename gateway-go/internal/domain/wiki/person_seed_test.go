package wiki

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedPersonPages(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// 박서연 already has a wiki page (with a title-suffix variant).
	if err := store.WritePage("인물/박서연.md", &Page{
		Meta: Frontmatter{ID: "park-seoyeon", Title: "박서연 차장", Category: "인물"},
		Body: "기존 페이지",
	}); err != nil {
		t.Fatal(err)
	}

	wd := &WikiDreamer{store: store, logger: slog.Default()}
	wd.SetPersonDirectory(func() []PersonSeed {
		return []PersonSeed{
			{Name: "김민준", Org: "현대차 구매팀", Phones: []string{"010-1234-5678"}},
			{Name: "박서연", Org: "남도에코"}, // exists → skip
			{Name: "민준", Org: "어딘가"},   // 2 runes → skip (false-positive guard)
			{Name: "이도윤", Org: "탑솔라"},  // mentioned once → skip
		}
	})

	input := "김민준 부장과 통화. 견적은 김민준 부장이 검토 후 회신. 박서연 차장도 참석. 박서연 차장이 실사 주관. 이도윤 과장 언급."
	created := wd.seedPersonPages(context.Background(), input)
	if created != 1 {
		t.Fatalf("want 1 seeded page, got %d", created)
	}

	page, err := store.ReadPage("인물/김민준.md")
	if err != nil {
		t.Fatalf("seeded page missing: %v", err)
	}
	if page.Meta.Category != "인물" || page.Meta.Confidence != "medium" {
		t.Errorf("unexpected meta: %+v", page.Meta)
	}
	if !strings.Contains(page.Body, "현대차 구매팀") || !strings.Contains(page.Body, "010-1234-5678") {
		t.Errorf("contact info missing from body: %q", page.Body)
	}

	// Idempotent: the next cycle must not duplicate.
	if again := wd.seedPersonPages(context.Background(), input); again != 0 {
		t.Errorf("re-run seeded %d duplicate pages", again)
	}
}
