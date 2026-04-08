package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestIndex_RenderAndParse(t *testing.T) {
	idx := NewIndex()
	idx.UpdateEntry("기술/dgx-spark.md", &Page{
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
	idx.UpdateEntry("사람/alice.md", &Page{
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
	if !strings.Contains(rendered, "id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks") {
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

	idx := NewIndex()
	idx.UpdateEntry("기술/go.md", &Page{
		Meta: Frontmatter{
			ID:         "go-lang",
			Title:      "Go",
			Summary:    "Deneb 주 개발 언어",
			Category:   "기술",
			Tags:       []string{"언어"},
			Importance: 0.7,
		},
	})
	idx.UpdateEntry("결정/wiki.md", &Page{
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

	reloaded := testutil.Must(ParseIndex(indexPath))

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

	idx := testutil.Must(ParseIndex(indexPath))

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

func TestIndex_RemoveEntry(t *testing.T) {
	idx := NewIndex()
	idx.UpdateEntry("기술/test.md", &Page{
		Meta: Frontmatter{Title: "Test", Category: "기술"},
	})
	if len(idx.Entries) != 1 {
		t.Fatalf("got %d, want 1 entry", len(idx.Entries))
	}

	idx.RemoveEntry("기술/test.md")
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
