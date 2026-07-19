package wikiwork

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// rawDataActivity must count only raw-data pages newer than the rep's
// updated: date — the freshness-SLO selection signal.
func TestRawDataActivity_CountsNewRawPagesOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), "")
	if err != nil {
		t.Fatal(err)
	}
	task := &wikiResearchTask{wikiStore: store}

	proj := filepath.Join(store.Dir(), "프로젝트", "테스트-현장")
	mailDir := filepath.Join(proj, "메일분석")
	if err := os.MkdirAll(mailDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(mailDir, "old.md")
	fresh := filepath.Join(mailDir, "fresh.md")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// updated: 10 days ago; "old" mail predates it, "fresh" is yesterday.
	updated := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -20)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	rep := "프로젝트/테스트-현장/대표.md"
	if got := task.rawDataActivity(rep, updated); got != 1 {
		t.Errorf("activity = %d, want 1 (only the fresh mail)", got)
	}
	// Unparseable updated date degrades to zero, never blocks selection.
	if got := task.rawDataActivity(rep, "unknown"); got != 0 {
		t.Errorf("activity with bad date = %d, want 0", got)
	}
	// Non-project path degrades to zero.
	if got := task.rawDataActivity("업무/기타.md", updated); got != 0 {
		t.Errorf("activity for non-project = %d, want 0", got)
	}
}
