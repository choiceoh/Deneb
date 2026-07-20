package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestMergeThemeRows_NewAndRecurring(t *testing.T) {
	rows := mergeThemeRows(nil, []ThemeSignal{
		{Key: "구리값-상승", Signal: "구리값 상승 추세", Evidence: "LME 보도"},
	}, "2026-07-01")
	if len(rows) != 1 || rows[0].Count != 1 || rows[0].First != "2026-07-01" {
		t.Fatalf("new signal should open a count-1 row: %+v", rows)
	}

	// Same day again — idempotent, no double count.
	rows = mergeThemeRows(rows, []ThemeSignal{{Key: "구리값-상승", Signal: "구리값 상승 추세"}}, "2026-07-01")
	if rows[0].Count != 1 {
		t.Errorf("same-day re-observation must not increment count: %+v", rows[0])
	}

	// Next day — count advances, First stays, Last moves.
	rows = mergeThemeRows(rows, []ThemeSignal{{Key: "구리값-상승", Signal: "구리값 재상승", Evidence: "새 근거"}}, "2026-07-05")
	if rows[0].Count != 2 || rows[0].First != "2026-07-01" || rows[0].Last != "2026-07-05" {
		t.Errorf("next-day re-observation should increment and move Last: %+v", rows[0])
	}
	if rows[0].Signal != "구리값 재상승" || rows[0].Evidence != "새 근거" {
		t.Errorf("latest phrasing/evidence should win: %+v", rows[0])
	}
}

func TestMergeThemeRows_DormancyAndReactivation(t *testing.T) {
	rows := []themeRow{{Key: "옛-신호", Signal: "옛 신호", First: "2026-01-01", Last: "2026-01-02", Count: 3, Status: "활성"}}
	rows = mergeThemeRows(rows, nil, "2026-07-01")
	if rows[0].Status != "휴면" {
		t.Errorf("stale row should turn 휴면: %+v", rows[0])
	}
	rows = mergeThemeRows(rows, []ThemeSignal{{Key: "옛-신호", Signal: "옛 신호 재점화"}}, "2026-07-01")
	if rows[0].Status != "활성" || rows[0].Count != 4 {
		t.Errorf("re-observation should reactivate: %+v", rows[0])
	}
}

func TestThemeStageLabels(t *testing.T) {
	for count, want := range map[int]string{1: "관찰", 2: "반복", 3: "반복", 4: "정착"} {
		if got := themeStage(count); got != want {
			t.Errorf("themeStage(%d) = %s, want %s", count, got, want)
		}
	}
}

func TestThemePageRenderParseRoundTrip(t *testing.T) {
	rows := []themeRow{
		{Key: "구리값-상승", Signal: "구리값 상승 추세 | 파이프 포함", First: "2026-07-01", Last: "2026-07-05", Count: 2, Status: "활성", Evidence: "LME 보도"},
		{Key: "야근-증가", Signal: "야근 증가", First: "2026-06-01", Last: "2026-06-02", Count: 1, Status: "휴면", Evidence: ""},
	}
	body := renderThemePage(rows)
	parsed := parseThemeRows(body)
	if len(parsed) != 2 {
		t.Fatalf("round trip lost rows: %d", len(parsed))
	}
	if parsed[0].Key != "구리값-상승" || parsed[0].Count != 2 || parsed[0].Last != "2026-07-05" {
		t.Errorf("row 0 mangled: %+v", parsed[0])
	}
	if strings.Contains(parsed[0].Signal, "|") {
		t.Errorf("pipe must be sanitized out of cells: %q", parsed[0].Signal)
	}
	if parsed[1].Status != "휴면" {
		t.Errorf("status should survive round trip: %+v", parsed[1])
	}
}

func TestPruneThemeRows_DropsDormantFirst(t *testing.T) {
	var rows []themeRow
	for i := 0; i < themeMaxRows; i++ {
		rows = append(rows, themeRow{Key: strings.Repeat("a", i+1), Status: "활성"})
	}
	rows = append(rows, themeRow{Key: "dormant-tail", Status: "휴면"})
	pruned := pruneThemeRows(rows)
	if len(pruned) != themeMaxRows {
		t.Fatalf("cap not enforced: %d", len(pruned))
	}
	for _, r := range pruned {
		if r.Key == "dormant-tail" {
			t.Error("dormant row should be pruned before active ones")
		}
	}
}

func TestParseThemeSignals_FencesAndKeyNormalization(t *testing.T) {
	signals, err := parseThemeSignals("```json\n[{\"key\":\"Copper Price\",\"signal\":\"구리|값 상승\",\"evidence\":\"근거\"},{\"key\":\"\",\"signal\":\"키 없음\"}]\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 {
		t.Fatalf("keyless entry should drop: %+v", signals)
	}
	if signals[0].Key != "copper-price" {
		t.Errorf("key should normalize: %q", signals[0].Key)
	}
	if strings.Contains(signals[0].Signal, "|") {
		t.Errorf("signal cell should be table-safe: %q", signals[0].Signal)
	}
}

func TestPrepareDreamUpdate_BlocksThemesLedger(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, ok := wd.prepareDreamUpdate(wikiUpdate{Action: "update", Path: ThemePagePath, Title: "반복 신호", Category: "업무", Content: "오염 시도"})
	if ok {
		t.Error("synthesis writes to the themes ledger must be blocked")
	}
}

func TestSaveThemePage_CreatesAndNoOps(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := renderThemePage([]themeRow{{Key: "구리값-상승", Signal: "구리값 상승", First: "2026-07-01", Last: "2026-07-01", Count: 1, Status: "활성"}})
	changed, err := wd.saveThemePage(body, "2026-07-01")
	if err != nil || !changed {
		t.Fatalf("first save should create: changed=%t err=%v", changed, err)
	}
	page, err := store.ReadPage(ThemePagePath)
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Category != "업무" || page.Meta.Type != "log" {
		t.Errorf("ledger frontmatter wrong: %+v", page.Meta)
	}
	if rows := wd.loadThemeRows(); len(rows) != 1 || rows[0].Key != "구리값-상승" {
		t.Errorf("loadThemeRows should parse the persisted table: %+v", rows)
	}

	// Unchanged body — no-op, no metadata churn.
	changed, err = wd.saveThemePage(body, "2026-07-02")
	if err != nil || changed {
		t.Errorf("identical body should be a no-op: changed=%t err=%v", changed, err)
	}
}
