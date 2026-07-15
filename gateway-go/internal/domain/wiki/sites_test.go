package wiki

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
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

// TestUpsertSitePage_CreateThenPartialEdit: create a 현장 page in the 공통 포맷, then
// a partial edit sets a later milestone without clobbering the earlier fields.
func TestUpsertSitePage_CreateThenPartialEdit(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	path, err := store.UpsertSitePage("군산수산리", "수산리", SiteFields{
		Client: "금호", Address: "전라북도 군산시 옥구읍 수산리.", Status: "계약",
		Capacity: 24, Kinds: []string{"루프탑"}, ContractDate: "2026-06-01",
	})
	if err != nil {
		t.Fatalf("UpsertSitePage create: %v", err)
	}
	if path != SitePagePath("군산수산리", "수산리") {
		t.Fatalf("path = %q", path)
	}
	page := testutil.Must(store.ReadPage(path))
	if page.Meta.Type != "site" || page.Meta.Address != "전북 군산시 옥구읍 수산리" ||
		page.Meta.Status != "계약" || page.Meta.Capacity != 24 || page.Meta.ContractDate != "2026-06-01" {
		t.Errorf("created site page meta = %+v", page.Meta)
	}
	if len(page.Meta.Kinds) != 1 || page.Meta.Kinds[0] != "태양광/루프탑" {
		t.Errorf("kinds = %v, want normalized [태양광/루프탑]", page.Meta.Kinds)
	}
	if !strings.Contains(page.Body, "## 공정 현황") {
		t.Errorf("new page missing standard body scaffold: %q", page.Body)
	}

	// Partial edit: advance status + add a later milestone; earlier fields survive.
	if _, err := store.UpsertSitePage("군산수산리", "수산리", SiteFields{
		Status: "개설", ConstructionStart: "2026-07-10",
	}); err != nil {
		t.Fatalf("UpsertSitePage edit: %v", err)
	}
	page = testutil.Must(store.ReadPage(path))
	if page.Meta.Status != "개설" || page.Meta.ConstructionStart != "2026-07-10" {
		t.Errorf("edit didn't apply: status=%q start=%q", page.Meta.Status, page.Meta.ConstructionStart)
	}
	if page.Meta.ContractDate != "2026-06-01" || page.Meta.Capacity != 24 || page.Meta.Address == "" {
		t.Errorf("partial edit clobbered earlier fields: %+v", page.Meta)
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

// TestIsProjectSitePageAndPath pins the 현장 slot's path shape + normalization.
func TestIsProjectSitePageAndPath(t *testing.T) {
	if got := SitePagePath("군산수산리", "수산리"); got != "프로젝트/군산수산리/현장/수산리.md" {
		t.Errorf("SitePagePath = %q", got)
	}
	yes := []string{"프로젝트/군산수산리/현장/수산리.md", "프로젝트/A/현장/B.md"}
	for _, p := range yes {
		if !IsProjectSitePage(p) {
			t.Errorf("IsProjectSitePage(%q) = false, want true", p)
		}
	}
	no := []string{
		"프로젝트/A/대표.md",          // rep page
		"프로젝트/A/현장.md",          // not under 현장/
		"프로젝트/거래/현장/x.md",       // reserved bucket owner
		"프로젝트/A/현장/sub/deep.md", // too deep
		"인물/김민준.md",             // wrong category
	}
	for _, p := range no {
		if IsProjectSitePage(p) {
			t.Errorf("IsProjectSitePage(%q) = true, want false", p)
		}
	}
	// A 현장 sub-path must survive path normalization (not fold into the filename).
	if got := NormalizeProjectPagePath("프로젝트/A/현장/B.md"); got != "프로젝트/A/현장/B.md" {
		t.Errorf("NormalizeProjectPagePath folded a 현장 slot: %q", got)
	}
	// Overdeep under 현장/ folds its tail into one filename (slash-debris guard).
	if got := NormalizeProjectPagePath("프로젝트/A/현장/B/C.md"); got != "프로젝트/A/현장/B-C.md" {
		t.Errorf("overdeep 현장 fold = %q", got)
	}
}

// TestProjectSites_SitePagesRichPlusRepFallback: ProjectSites emits one rich row
// per 현장 page (own status·용량), then falls back to 대표페이지 Sites for addresses
// with no 현장 page — so nothing disappears during migration.
func TestProjectSites_SitePagesRichPlusRepFallback(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	// Project A: rep carries two sites + project-level kinds/capacity; one of the
	// two gets a rich 현장 page, the other stays a fallback pin.
	mustWrite(t, store, "프로젝트/A/대표.md", &Page{
		Meta: Frontmatter{
			Title: "A", Client: "금호", Type: "project",
			Sites:    []string{"전북 군산시 옥구읍 수산리", "충남 당진시 석문면"},
			Kinds:    []string{"태양광/토지"},
			Capacity: 50,
		},
		Body: "# A",
	})
	mustWrite(t, store, SitePagePath("A", "수산리"), &Page{
		Meta: Frontmatter{
			Title: "수산리", Type: "site",
			Address: "전북 군산시 옥구읍 수산리", Status: "계약", Capacity: 24,
			Kinds: []string{"태양광/루프탑"},
		},
		Body: "현장.",
	})
	// Project B: no 현장 page → pure fallback.
	mustWrite(t, store, "프로젝트/B/대표.md", &Page{
		Meta: Frontmatter{Title: "B", Type: "project", Sites: []string{"경남 밀양시 부북면"}},
		Body: "# B",
	})

	rows, err := store.ProjectSites()
	if err != nil {
		t.Fatalf("ProjectSites: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ProjectSites() = %d rows, want 3: %+v", len(rows), rows)
	}
	// Rows sort by project name then address: A/석문면(fallback), A/수산리(page), B/부북면.
	byAddr := map[string]ProjectSite{}
	for _, r := range rows {
		byAddr[firstOf(r.Sites)] = r
	}
	sur := byAddr["전북 군산시 옥구읍 수산리"]
	if sur.Status != "계약" || sur.Capacity != 24 || sur.Path != SitePagePath("A", "수산리") {
		t.Errorf("수산리 (site page) = %+v, want status 계약 / cap 24 / site-page path", sur)
	}
	if len(sur.Kinds) != 1 || sur.Kinds[0] != "태양광/루프탑" {
		t.Errorf("수산리 kinds = %v, want site-page [태양광/루프탑]", sur.Kinds)
	}
	sok := byAddr["충남 당진시 석문면"]
	if sok.Status != "" || sok.Capacity != 50 || sok.Path != "프로젝트/A/대표.md" {
		t.Errorf("석문면 (fallback) = %+v, want blank status / rep cap 50 / rep path", sok)
	}
	if _, ok := byAddr["경남 밀양시 부북면"]; !ok {
		t.Errorf("B fallback site missing: %+v", rows)
	}
}

// TestProjectSites_SitePagesKeyedByFolderNotTitle: 현장 pages must resolve to their
// owner by FOLDER slug, not the (often different) 대표페이지 title — else a titled
// project loses all its rich site rows.
func TestProjectSites_SitePagesKeyedByFolderNotTitle(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	// Folder slug "군산수산리" but a human title that differs.
	mustWrite(t, store, "프로젝트/군산수산리/대표.md", &Page{
		Meta: Frontmatter{Title: "군산 수산리 태양광 발전소", Type: "project"},
		Body: "# 군산",
	})
	mustWrite(t, store, SitePagePath("군산수산리", "수산리"), &Page{
		Meta: Frontmatter{
			Title: "수산리", Type: "site",
			Address: "전북 군산시 옥구읍 수산리", Status: "개설", Capacity: 24,
		},
		Body: "현장.",
	})

	rows, err := store.ProjectSites()
	if err != nil {
		t.Fatalf("ProjectSites: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ProjectSites() = %d rows, want 1 (the 현장 page): %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Name != "군산 수산리 태양광 발전소" || r.Status != "개설" || r.Capacity != 24 {
		t.Errorf("row = %+v, want titled project name + 개설/24 from the 현장 page", r)
	}
	if r.Path != SitePagePath("군산수산리", "수산리") {
		t.Errorf("path = %q, want the 현장 page path (not the 대표페이지)", r.Path)
	}
}
