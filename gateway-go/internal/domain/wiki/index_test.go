package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestIndex_RenderAndParse(t *testing.T) {
	idx := newIndex()
	idx.updateEntry("기술/dgx-spark.md", &Page{
		Meta: Frontmatter{
			ID:         "dgx-spark",
			Title:      "DGX Spark",
			Summary:    "128GB 로컬 AI 서버",
			Category:   "기술",
			Tags:       []string{"하드웨어", "NVIDIA"},
			Related:    []string{"기술/go.md"},
			Importance: 0.9,
			Updated:    "2026-04-06",
		},
	})
	idx.updateEntry("사람/alice.md", &Page{
		Meta: Frontmatter{
			Title:    "Alice",
			Category: "사람",
			Tags:     []string{"팀원"},
			Updated:  "2026-03-01",
		},
	})
	idx.LastProcessed = "2026-04-05"

	rendered := idx.Render()

	// Verify TSV structure.
	if !strings.Contains(rendered, "# 위키 인덱스") {
		t.Error("missing header")
	}
	if !strings.Contains(rendered, "마지막 일지 처리: 2026-04-05") {
		t.Error("missing last processed date")
	}
	// TSV header row.
	if !strings.Contains(rendered, "id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks\tcreated") {
		t.Error("missing TSV header row")
	}
	// TSV data should contain the entry fields.
	if !strings.Contains(rendered, "dgx-spark\t기술/dgx-spark.md\tDGX Spark\t128GB 로컬 AI 서버") {
		t.Errorf("missing TSV data for dgx-spark in:\n%s", rendered)
	}
	if !strings.Contains(rendered, "0.90") {
		t.Error("missing importance value")
	}
}

func TestIndex_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")

	idx := newIndex()
	idx.updateEntry("기술/go.md", &Page{
		Meta: Frontmatter{
			ID:         "go-lang",
			Title:      "Go",
			Summary:    "Deneb 주 개발 언어",
			Category:   "기술",
			Tags:       []string{"언어"},
			Importance: 0.7,
		},
	})
	idx.updateEntry("결정/wiki.md", &Page{
		Meta: Frontmatter{
			ID:         "wiki-switch",
			Title:      "위키 전환",
			Summary:    "Karpathy 위키 스타일로 전환",
			Category:   "결정",
			Importance: 0.9,
		},
	})

	if err := idx.Save(indexPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := testutil.Must(parseIndex(indexPath))

	if len(reloaded.Entries) != 2 {
		t.Errorf("reloaded %d entries, want 2", len(reloaded.Entries))
	}

	goEntry, ok := reloaded.Entries["기술/go.md"]
	if !ok {
		t.Fatal("missing go.md entry")
	}
	if goEntry.ID != "go-lang" {
		t.Errorf("go id = %q", goEntry.ID)
	}
	if goEntry.Title != "Go" {
		t.Errorf("go title = %q", goEntry.Title)
	}
	if goEntry.Summary != "Deneb 주 개발 언어" {
		t.Errorf("go summary = %q", goEntry.Summary)
	}
	if goEntry.Category != "기술" {
		t.Errorf("go category = %q", goEntry.Category)
	}
	if goEntry.Importance != 0.7 {
		t.Errorf("go importance = %f, want 0.7", goEntry.Importance)
	}

	wikiEntry, ok := reloaded.Entries["결정/wiki.md"]
	if !ok {
		t.Fatal("missing wiki.md entry")
	}
	if wikiEntry.ID != "wiki-switch" {
		t.Errorf("wiki id = %q", wikiEntry.ID)
	}
	if wikiEntry.Importance != 0.9 {
		t.Errorf("wiki importance = %f, want 0.9", wikiEntry.Importance)
	}
}

// TestIndex_CreatedPersistsAcrossReload pins the created column: NewStore
// restores the index from index.md on every gateway restart, and
// ActiveCounterpartyDomains keys its recency window off Created (falling back
// to Updated only when absent). Before the column existed, every restart
// blanked Created and the Updated fallback re-activated stale sender domains
// whenever an old mail was reclassified (metadata churn re-stamps Updated).
func TestIndex_CreatedPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")

	idx := newIndex()
	idx.updateEntry("프로젝트/기아/메일분석/m1.md", &Page{
		Meta: Frontmatter{
			ID:       "m1",
			Title:    "견적 요청",
			Category: "프로젝트",
			Tags:     []string{"acme.co.kr"},
			Updated:  "2026-07-01",
			Created:  "2026-05-02",
		},
	})
	if err := idx.Save(indexPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := testutil.Must(parseIndex(indexPath))
	entry, ok := reloaded.Entries["프로젝트/기아/메일분석/m1.md"]
	if !ok {
		t.Fatal("missing entry after reload")
	}
	if entry.Created != "2026-05-02" {
		t.Errorf("Created = %q, want it to survive the render→parse roundtrip", entry.Created)
	}
	if entry.Updated != "2026-07-01" {
		t.Errorf("Updated = %q, want 2026-07-01", entry.Updated)
	}
}

// TestParseTSVLine_OldFormatWithoutCreated: a pre-created-column line (10
// fields ending at backlinks) parses with Created empty — the callers'
// Updated fallback covers it.
func TestParseTSVLine_OldFormatWithoutCreated(t *testing.T) {
	line := "m1\t프로젝트/기아/메일분석/m1.md\t견적 요청\t요약\tacme.co.kr\t0.50\t2026-07-01\tsource\thigh\t3"
	got := parseTSVLine(line, "프로젝트")
	if got.path != "프로젝트/기아/메일분석/m1.md" {
		t.Fatalf("path = %q", got.path)
	}
	if got.entry.Created != "" {
		t.Errorf("Created = %q, want empty for the old 10-field format", got.entry.Created)
	}
	if got.entry.Updated != "2026-07-01" {
		t.Errorf("Updated = %q", got.entry.Updated)
	}
	if got.entry.Type != "source" || got.entry.Confidence != "high" {
		t.Errorf("type/confidence = %q/%q, want source/high", got.entry.Type, got.entry.Confidence)
	}
}

func TestParseIndex_LegacyFormat(t *testing.T) {
	// Verify backward compatibility with old markdown list format.
	legacy := `# 위키 인덱스

_자동 생성: 2026-04-07 14:30_

마지막 일지 처리: 2026-04-05

## 기술

- [[기술/dgx-spark.md]] — DGX Spark [하드웨어, NVIDIA] (i:0.90, u:2026-04-06)
- [[기술/go.md]] — Go [언어] (i:0.70)
`
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.md")
	if err := os.WriteFile(indexPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := testutil.Must(parseIndex(indexPath))

	if idx.LastProcessed != "2026-04-05" {
		t.Errorf("LastProcessed = %q", idx.LastProcessed)
	}
	if len(idx.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(idx.Entries))
	}

	spark, ok := idx.Entries["기술/dgx-spark.md"]
	if !ok {
		t.Fatal("missing dgx-spark entry")
	}
	if spark.Title != "DGX Spark" {
		t.Errorf("title = %q", spark.Title)
	}
	if spark.Importance != 0.9 {
		t.Errorf("importance = %f", spark.Importance)
	}
}

func TestIndex_DeletesEntry(t *testing.T) {
	idx := newIndex()
	idx.updateEntry("기술/test.md", &Page{
		Meta: Frontmatter{Title: "Test", Category: "기술"},
	})
	if len(idx.Entries) != 1 {
		t.Fatalf("got %d, want 1 entry", len(idx.Entries))
	}

	idx.removeEntry("기술/test.md")
	if len(idx.Entries) != 0 {
		t.Errorf("got %d, want 0 entries after remove", len(idx.Entries))
	}
}

func TestParseIndexLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		cat  string
		want indexRenderEntry
	}{
		{
			name: "new format with importance and updated",
			line: "- [[기술/dgx-spark.md]] — DGX Spark [하드웨어, NVIDIA] (i:0.90, u:2026-04-06)",
			cat:  "기술",
			want: indexRenderEntry{
				path: "기술/dgx-spark.md",
				entry: IndexEntry{
					Title:      "DGX Spark",
					Category:   "기술",
					Tags:       []string{"하드웨어", "NVIDIA"},
					Importance: 0.9,
					Updated:    "2026-04-06",
				},
			},
		},
		{
			name: "legacy format with star marker",
			line: "- [[기술/dgx-spark.md]] — DGX Spark [하드웨어, NVIDIA] *",
			cat:  "기술",
			want: indexRenderEntry{
				path: "기술/dgx-spark.md",
				entry: IndexEntry{
					Title:      "DGX Spark",
					Category:   "기술",
					Tags:       []string{"하드웨어", "NVIDIA"},
					Importance: 0.85,
				},
			},
		},
		{
			name: "no importance",
			line: "- [[사람/alice.md]] — Alice",
			cat:  "사람",
			want: indexRenderEntry{
				path: "사람/alice.md",
				entry: IndexEntry{
					Title:    "Alice",
					Category: "사람",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseIndexLine(tc.line, tc.cat)
			if got.path != tc.want.path {
				t.Errorf("path = %q, want %q", got.path, tc.want.path)
			}
			if got.entry.Title != tc.want.entry.Title {
				t.Errorf("title = %q, want %q", got.entry.Title, tc.want.entry.Title)
			}
			if got.entry.Importance != tc.want.entry.Importance {
				t.Errorf("importance = %f, want %f", got.entry.Importance, tc.want.entry.Importance)
			}
			if got.entry.Updated != tc.want.entry.Updated {
				t.Errorf("updated = %q, want %q", got.entry.Updated, tc.want.entry.Updated)
			}
		})
	}
}

func TestParseTSVLine_KeepsEveryTypeTheDomainWrites(t *testing.T) {
	// The restore guard drifted behind the writers: deals.go writes "deal",
	// sites.go "site", project_status.go/restructure.go "project". A rejected
	// value is silently blanked here and the next Save persists the loss, so
	// every value the package writes must survive the round trip.
	for _, want := range []string{"concept", "entity", "source", "comparison", "log", "deal", "site", "project"} {
		line := "p1	프로젝트/x.md	제목	요약	태그	0.50	2026-07-01	" + want + "	high	3"
		if got := parseTSVLine(line, "프로젝트").entry.Type; got != want {
			t.Errorf("type %q was dropped on restore (got %q)", want, got)
		}
	}
}

func TestParseTSVLine_StillRejectsTheOldNumericColumn(t *testing.T) {
	// Field 7 held the backlinks COUNT before the type column existed. The
	// guard is what keeps that number from being read as a type — widening the
	// vocabulary must not cost that.
	line := "p1	프로젝트/x.md	제목	요약	태그	0.50	2026-07-01	7"
	if got := parseTSVLine(line, "프로젝트").entry.Type; got != "" {
		t.Errorf("old-format backlinks count %q was read as a type", got)
	}
	// Free-form values (an LLM once wrote "preference") stay rejected too.
	line = "p1	프로젝트/x.md	제목	요약	태그	0.50	2026-07-01	preference	high	3"
	if got := parseTSVLine(line, "프로젝트").entry.Type; got != "" {
		t.Errorf("free-form type %q was accepted", got)
	}
}
