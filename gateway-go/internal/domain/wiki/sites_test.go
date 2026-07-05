package wiki

import (
	"path/filepath"
	"testing"
)

func TestNormalizeSiteName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"전북 군산시 옥구읍 수산리", "전북 군산시 옥구읍 수산리"},
		{"전라북도 군산시 옥구읍 수산리.", "전북 군산시 옥구읍 수산리"}, // full province + trailing period
		{"전북특별자치도 군산시", "전북 군산시"},
		{"서울특별시 강남구", "서울 강남구"},
		{"  충남   당진시  송악읍 ", "충남 당진시 송악읍"}, // whitespace collapse
		{"군산시 옥구읍", "군산시 옥구읍"},             // province unknown — kept as written
		{"제주특별자치도", "제주"},
		{"", ""},
		{" . ", ""},
	}
	for _, tc := range cases {
		if got := normalizeSiteName(tc.in); got != tc.want {
			t.Errorf("normalizeSiteName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFrontmatterSitesRoundtrip: sites survive Render→Parse and arrive
// normalized regardless of how the writer spelled the province.
func TestFrontmatterSitesRoundtrip(t *testing.T) {
	p := &Page{
		Meta: Frontmatter{
			Title: "군산 수산리 태양광",
			Sites: []string{"전라북도 군산시 옥구읍 수산리.", "충남 당진시 송악읍"},
			// Synonyms fold onto the enum; out-of-vocabulary values drop.
			Kinds: []string{"EPC", "모듈", "루프탑", "모듈"},
		},
		Body: "본문.",
	}
	parsed, err := ParsePage(p.Render())
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	want := []string{"전북 군산시 옥구읍 수산리", "충남 당진시 송악읍"}
	if len(parsed.Meta.Sites) != 2 || parsed.Meta.Sites[0] != want[0] || parsed.Meta.Sites[1] != want[1] {
		t.Errorf("sites roundtrip = %v, want %v", parsed.Meta.Sites, want)
	}
	wantKinds := []string{"시공", "모듈"}
	if len(parsed.Meta.Kinds) != 2 || parsed.Meta.Kinds[0] != wantKinds[0] || parsed.Meta.Kinds[1] != wantKinds[1] {
		t.Errorf("kinds roundtrip = %v, want %v (synonym folded, 루프탑 dropped, deduped)", parsed.Meta.Kinds, wantKinds)
	}
}

// TestProjectAnchor_SiteMatch: naming the PLACE anchors the project — mail and
// calendar text says "수산리 현장" far more often than the project title.
func TestProjectAnchor_SiteMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustWrite(t, store, "프로젝트/군산-수산리-태양광/대표.md", &Page{
		Meta: Frontmatter{
			Title: "군산 수산리 태양광 발전소",
			Sites: []string{"전북 군산시 옥구읍 수산리"},
		},
		Body: "개요.",
	})

	for _, text := range []string{
		"수산리 현장 방문 결과 공유해줘",
		"전북 군산시 옥구읍 수산리 개발행위허가 어떻게 됐지",
	} {
		refs := store.MatchProjectsInText(text, 2)
		if len(refs) != 1 || refs[0].Name != "군산 수산리 태양광 발전소" {
			t.Errorf("site anchor for %q = %+v", text, refs)
		}
	}
	if refs := store.MatchProjectsInText("당진 케이블 견적", 2); len(refs) != 0 {
		t.Errorf("unrelated text must not anchor, got %+v", refs)
	}
}
