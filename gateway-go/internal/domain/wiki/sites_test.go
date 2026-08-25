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
	parsed, err := parsePage(p.Render())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	want := []string{"전북 군산시 옥구읍 수산리", "충남 당진시 송악읍"}
	if len(parsed.Meta.Sites) != 2 || parsed.Meta.Sites[0] != want[0] || parsed.Meta.Sites[1] != want[1] {
		t.Errorf("sites roundtrip = %v, want %v", parsed.Meta.Sites, want)
	}
	// EPC→태양광(stage word) but 루프탑 upgrades to 태양광/루프탑 which
	// subsumes the bare parent; 모듈 upgrades to 기자재/모듈; dup dropped.
	wantKinds := []string{"기자재/모듈", "태양광/루프탑"}
	if len(parsed.Meta.Kinds) != 2 || parsed.Meta.Kinds[0] != wantKinds[0] || parsed.Meta.Kinds[1] != wantKinds[1] {
		t.Errorf("kinds roundtrip = %v, want %v", parsed.Meta.Kinds, wantKinds)
	}
}

// TestNormalizeKindsHierarchy pins the two-level vocabulary semantics.
func TestNormalizeKindsHierarchy(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		// Flat legacy values auto-upgrade (시공·개발 → 태양광).
		{[]string{"모듈", "시공"}, []string{"기자재/모듈", "태양광"}},
		{[]string{"개발"}, []string{"태양광"}},
		// Bare child words fold under their parent.
		{[]string{"루프탑"}, []string{"태양광/루프탑"}},
		{[]string{"해상풍력"}, []string{"풍력/해상"}},
		// Parent + its child → child only (parent implied).
		{[]string{"태양광", "시공/루프탑"}, []string{"태양광/루프탑"}},
		// Parent alone stays (2차 미상).
		{[]string{"기자재"}, []string{"기자재"}},
		// Parent + OTHER family's child keeps both.
		{[]string{"기자재", "시공/토지"}, []string{"기자재", "태양광/토지"}},
		// ESS classifies under 태양광 (발전소 사업), not 기자재 (operator ruling).
		{[]string{"BESS"}, []string{"태양광/ESS"}},
		// 용역/협력 live under the 기타 primary.
		{[]string{"용역", "협력"}, []string{"기타/용역", "기타/협력"}},
		// Stage word next to explicit 풍력 drops instead of minting 태양광 —
		// a wind development project is not a solar project.
		{[]string{"풍력", "개발"}, []string{"풍력"}},
		{[]string{"풍력/육상", "시공"}, []string{"풍력/육상"}},
		// …but an explicit 태양광 next to 풍력 stays (genuinely mixed).
		{[]string{"풍력", "태양광"}, []string{"풍력", "태양광"}},
		// Out-of-vocabulary drops; dedupe after folding.
		{[]string{"자가소비", "mod", "기자재/모듈"}, []string{"기자재/모듈"}},
	}
	for _, tc := range cases {
		got := normalizeKinds(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("normalizeKinds(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("normalizeKinds(%v) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

// TestProjectAnchor_MatchesBySiteNameMentionIgnoresUnrelatedText: naming the PLACE
// anchors the project — mail and calendar text says "수산리 현장" far more often
// than the project title.
func TestProjectAnchor_MatchesBySiteNameMentionIgnoresUnrelatedText(t *testing.T) {
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

// TestFrontmatterAddressStatusRoundtrip: a 현장 page's address (normalized like
// Sites) and status survive Render→Parse.
func TestFrontmatterAddressStatusRoundtrip(t *testing.T) {
	p := &Page{
		Meta: Frontmatter{
			Title:                "수산리",
			Type:                 "site",
			Address:              "전라북도 군산시 옥구읍 수산리.", // full province + trailing period → normalized
			Status:               "개설",
			Capacity:             24,
			Kinds:                []string{"루프탑"},
			ContractDate:         "2026-06-01",
			ConstructionStart:    "2026-07-10",
			ModuleDelivery:       "2026-08-01~2026-08-15",
			PreUseInspection:     "2026-09-20",
			CompletionInspection: "2026-10-05",
		},
		Body: "현장 메모.",
	}
	parsed, err := parsePage(p.Render())
	if err != nil {
		t.Fatalf("parsePage: %v", err)
	}
	if parsed.Meta.Address != "전북 군산시 옥구읍 수산리" {
		t.Errorf("address roundtrip = %q, want normalized 전북 군산시 옥구읍 수산리", parsed.Meta.Address)
	}
	if parsed.Meta.Status != "개설" {
		t.Errorf("status roundtrip = %q, want 개설", parsed.Meta.Status)
	}
	if parsed.Meta.Capacity != 24 {
		t.Errorf("capacity roundtrip = %v, want 24", parsed.Meta.Capacity)
	}
	if parsed.Meta.ContractDate != "2026-06-01" || parsed.Meta.ConstructionStart != "2026-07-10" ||
		parsed.Meta.ModuleDelivery != "2026-08-01~2026-08-15" || parsed.Meta.PreUseInspection != "2026-09-20" ||
		parsed.Meta.CompletionInspection != "2026-10-05" {
		t.Errorf("milestone roundtrip lost data: %+v", parsed.Meta)
	}
}
