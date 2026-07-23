package wikitool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// TestWikiReadShowsResolvedProvenanceFooter verifies the read path turns a
// page's raw episode refs into an actionable citation block: the diary date it
// came from, whether the diary still exists, and the exact daily call to verify.
func TestWikiReadShowsResolvedProvenanceFooter(t *testing.T) {
	dir := t.TempDir()
	diaryDir := filepath.Join(dir, "diary")
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := os.WriteFile(filepath.Join(diaryDir, "diary-2026-07-20.md"), []byte("구리 현물가 메모"), 0o644); err != nil {
		t.Fatal(err)
	}

	page := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID: "copper", Title: "구리 가격", Category: "업무",
			Sources: []string{"d2026-07-20#aabbccdd", "d2026-01-01#00000000"},
		},
		Body: "현물가 메모.",
	}
	if err := store.WritePage("업무/구리-가격.md", page); err != nil {
		t.Fatal(err)
	}

	out, err := wikiReadRange(context.Background(), store, "업무/구리-가격.md", "", 0, 0)
	if err != nil {
		t.Fatalf("wikiReadRange: %v", err)
	}
	if !strings.Contains(out, "출처(provenance)") {
		t.Fatalf("provenance footer missing:\n%s", out)
	}
	// Present diary → actionable verify hint with the exact daily call.
	if !strings.Contains(out, "wiki action=daily date=2026-07-20") {
		t.Errorf("resolved-source verify hint missing:\n%s", out)
	}
	// Rotated-away diary → flagged, not a broken link.
	if !strings.Contains(out, "2026-01-01 다이어리 (원본 정리됨") {
		t.Errorf("dangling-source note missing:\n%s", out)
	}

	// Partial reads (section + line range) must carry the footer too — search
	// steers the agent to range reads, so the loop has to work there.
	page.Body = "## 현황\n현물가 상향."
	if err := store.WritePage("업무/구리-가격.md", page); err != nil {
		t.Fatal(err)
	}
	sec, err := wikiReadRange(context.Background(), store, "업무/구리-가격.md", "현황", 0, 0)
	if err != nil {
		t.Fatalf("section read: %v", err)
	}
	if !strings.Contains(sec, "wiki action=daily date=2026-07-20") {
		t.Errorf("section read dropped provenance footer:\n%s", sec)
	}
	rng, err := wikiReadRange(context.Background(), store, "업무/구리-가격.md", "", 1, 3)
	if err != nil {
		t.Fatalf("range read: %v", err)
	}
	if !strings.Contains(rng, "wiki action=daily date=2026-07-20") {
		t.Errorf("line-range read dropped provenance footer:\n%s", rng)
	}
}

// TestWikiDailyDateTargetsOneDiary verifies the date param reads exactly that
// diary day (the provenance verify path) and guards against a traversal date.
func TestWikiDailyDateTargetsOneDiary(t *testing.T) {
	dir := t.TempDir()
	diaryDir := filepath.Join(dir, "diary")
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, d := range []string{"2026-07-19", "2026-07-20"} {
		if err := os.WriteFile(filepath.Join(diaryDir, "diary-"+d+".md"), []byte("일지 "+d), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	call := func(params map[string]any) string {
		t.Helper()
		fn := ToolWiki(&tooldeps.WikiDeps{Store: store}, dir)
		raw, _ := json.Marshal(params)
		out, err := fn(context.Background(), raw)
		if err != nil {
			t.Fatalf("ToolWiki: %v", err)
		}
		return out
	}

	// Date-targeted: only that day, not the other.
	out := call(map[string]any{"action": "daily", "date": "2026-07-20"})
	if !strings.Contains(out, "일지 2026-07-20") || strings.Contains(out, "일지 2026-07-19") {
		t.Errorf("date targeting wrong:\n%s", out)
	}
	// Missing date → clean "no diary" message, not an error.
	if miss := call(map[string]any{"action": "daily", "date": "2026-06-06"}); !strings.Contains(miss, "일지 없음") {
		t.Errorf("missing-date message wrong: %q", miss)
	}
	// Traversal date → rejected by the guard.
	if bad := call(map[string]any{"action": "daily", "date": "../../etc/passwd"}); !strings.Contains(bad, "잘못된 날짜 형식") {
		t.Errorf("traversal date not rejected: %q", bad)
	}

	// A long day must be fully reachable via paging — no silent prefix cut.
	var long strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&long, "L%d 항목\n", i)
	}
	if err := os.WriteFile(filepath.Join(diaryDir, "diary-2026-08-01.md"), []byte(long.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	page1 := call(map[string]any{"action": "daily", "date": "2026-08-01"})
	if !strings.Contains(page1, "[계속:") || !strings.Contains(page1, "from_line=") {
		t.Errorf("long diary missing continuation hint:\n%s", page1[:min(len(page1), 400)])
	}
	if strings.Contains(page1, "L500 항목") {
		t.Errorf("first window should not already contain the tail line")
	}
	// The continuation window must reach the tail.
	tail := call(map[string]any{"action": "daily", "date": "2026-08-01", "from_line": 400})
	if !strings.Contains(tail, "L500 항목") {
		t.Errorf("continuation window did not reach the diary tail:\n%s", tail)
	}
}
