package wiki

import (
	"path/filepath"
	"testing"
)

// TestMatchCounterpartiesInTextReturnsMatches pins the ledger-anchor matching
// rules: Korean names by normalized containment, long ASCII names by
// containment, short ASCII names only as standalone tokens (with an attached
// Korean particle allowed), archived ledgers excluded, invalid legacy
// supersession flags ignored, longest key first.
func TestMatchCounterpartiesInTextReturnsMatches(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/거래/대한전선.md", &Page{
		Meta: Frontmatter{Title: "대한전선", Summary: "케이블 공급 거래처"},
		Body: "- 2026-06: 345kV 견적 회신",
	})
	mustWrite(t, store, "프로젝트/거래/sunkean.md", &Page{
		Meta: Frontmatter{Title: "Sunkean"},
		Body: "솔라케이블 협의.",
	})
	mustWrite(t, store, "프로젝트/거래/skb.md", &Page{
		Meta: Frontmatter{Title: "SKB"},
		Body: "회선 계약.",
	})
	mustWrite(t, store, "프로젝트/거래/hre.md", &Page{
		Meta: Frontmatter{Title: "HRE"},
		Body: "협력 검토.",
	})
	mustWrite(t, store, "프로젝트/거래/옛거래처.md", &Page{
		Meta: Frontmatter{Title: "옛거래처", Archived: true},
		Body: "종료된 거래.",
	})
	// Deal ledgers can never be a valid old-version relationship. Preserve an
	// old bad flag as audit metadata without retiring the live anchor.
	mustWrite(t, store, "프로젝트/거래/합병전거래처.md", &Page{
		Meta: Frontmatter{Title: "합병전거래처", SupersededBy: "프로젝트/거래/대한전선.md"},
		Body: "병합됨.",
	})

	cases := []struct {
		name  string
		text  string
		limit int
		want  []string // expected ref names in order
	}{
		{"korean containment", "대한전선 견적 어떻게 됐어?", 2, []string{"대한전선"}},
		{"long ascii containment", "sunkean 케이블 협의 진행 중이야", 2, []string{"Sunkean"}},
		{"short ascii token", "SKB 회신 왔어?", 2, []string{"SKB"}},
		{"short ascii with particle", "SKB에 자료 보냈나", 2, []string{"SKB"}},
		{"short ascii inside word rejected", "the threshold value is fine", 2, nil},
		{"short ascii with ascii tail rejected", "skbroadband 문의", 2, nil},
		{"archived skipped", "옛거래처 정리하자", 2, nil},
		{"invalid legacy supersession stays active", "합병전거래처 근황 알려줘", 2, []string{"합병전거래처"}},
		{"unknown", "없는회사 이야기", 2, nil},
		{"longest key first", "대한전선이랑 sunkean 둘 다 확인해줘", 1, []string{"Sunkean"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := store.MatchCounterpartiesInText(tc.text, tc.limit)
			if len(got) != len(tc.want) {
				t.Fatalf("MatchCounterpartiesInText(%q) = %+v, want names %v", tc.text, got, tc.want)
			}
			for i := range got {
				if got[i].Name != tc.want[i] {
					t.Errorf("hit[%d] = %q, want %q", i, got[i].Name, tc.want[i])
				}
			}
		})
	}

	// Ref carries the ledger path + summary for the recall note.
	refs := store.MatchCounterpartiesInText("대한전선 근황", 1)
	if len(refs) != 1 || refs[0].Path != "프로젝트/거래/대한전선.md" || refs[0].Summary == "" {
		t.Errorf("ref fields incomplete: %+v", refs)
	}
}
