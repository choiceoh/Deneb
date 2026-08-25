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

// TestSetSiteStatus_SetsAndClears: SetSiteStatus overwrites lifecycle including clear
// to 미분류 (empty), and rejects non-현장 paths / unknown labels.
func TestSetSiteStatus_SetsAndClears(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	path, err := store.UpsertSitePage("군산수산리", "수산리", SiteFields{
		Address: "전북 군산시 옥구읍 수산리", Status: "계약",
	})
	if err != nil {
		t.Fatalf("UpsertSitePage: %v", err)
	}
	if err := store.SetSiteStatus(path, "개설"); err != nil {
		t.Fatalf("SetSiteStatus 개설: %v", err)
	}
	page := testutil.Must(store.ReadPage(path))
	if page.Meta.Status != "개설" {
		t.Fatalf("status = %q, want 개설", page.Meta.Status)
	}
	if err := store.SetSiteStatus(path, ""); err != nil {
		t.Fatalf("SetSiteStatus clear: %v", err)
	}
	page = testutil.Must(store.ReadPage(path))
	if page.Meta.Status != "" {
		t.Fatalf("cleared status = %q, want empty", page.Meta.Status)
	}
	if err := store.SetSiteStatus("프로젝트/군산수산리/대표.md", "개설"); err == nil {
		t.Fatal("expected reject for 대표페이지 path")
	}
	if err := store.SetSiteStatus(path, "공사중"); err == nil {
		t.Fatal("expected reject for unknown status label")
	}
}

// TestEnsureSitePage_CreatesThenIdempotent: EnsureSitePage bootstraps one stub from a
// 대표페이지 path + address, copies 거래처·kinds, and returns the same path on repeat.
func TestEnsureSitePage_CreatesThenIdempotent(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	rep := "프로젝트/군산수산리/대표.md"
	mustWrite(t, store, rep, &Page{
		Meta: Frontmatter{
			Title: "군산 수산리 태양광", Type: "project", Client: "금호",
			Sites: []string{"전북 군산시 옥구읍 수산리"},
			Kinds: []string{"태양광/토지"},
		},
		Body: "# 군산",
	})
	path, created, err := store.EnsureSitePage(rep, "전라북도 군산시 옥구읍 수산리.")
	if err != nil {
		t.Fatalf("EnsureSitePage: %v", err)
	}
	if !created || path != SitePagePath("군산수산리", "수산리") {
		t.Fatalf("created=%v path=%q, want new 수산리 page", created, path)
	}
	page := testutil.Must(store.ReadPage(path))
	if page.Meta.Address != "전북 군산시 옥구읍 수산리" || page.Meta.Client != "금호" ||
		len(page.Meta.Kinds) != 1 || page.Meta.Kinds[0] != "태양광/토지" {
		t.Errorf("stub meta = %+v", page.Meta)
	}
	again, created2, err := store.EnsureSitePage(rep, "전북 군산시 옥구읍 수산리")
	if err != nil || created2 || again != path {
		t.Fatalf("idempotent EnsureSitePage = (%q,%v,%v), want (%q,false,nil)", again, created2, err, path)
	}
}

// TestUpdateSitePage_PartialMilestones: UpdateSitePage writes milestone dates by path
// without clobbering status/address, and rejects 대표페이지 paths.
func TestUpdateSitePage_PartialMilestones(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	path, err := store.UpsertSitePage("군산수산리", "수산리", SiteFields{
		Address: "전북 군산시 옥구읍 수산리", Status: "계약", ContractDate: "2026-06-01",
	})
	if err != nil {
		t.Fatalf("UpsertSitePage: %v", err)
	}
	if err := store.UpdateSitePage(path, SiteFields{ConstructionStart: "2026-07-10"}); err != nil {
		t.Fatalf("UpdateSitePage: %v", err)
	}
	page := testutil.Must(store.ReadPage(path))
	if page.Meta.ConstructionStart != "2026-07-10" || page.Meta.Status != "계약" ||
		page.Meta.ContractDate != "2026-06-01" {
		t.Errorf("after update meta = %+v", page.Meta)
	}
	if err := store.UpdateSitePage("프로젝트/군산수산리/대표.md", SiteFields{ContractDate: "2026-01-01"}); err == nil {
		t.Fatal("expected reject for 대표페이지 path")
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

// TestSeedSitePages_BootstrapsUncoveredIdempotent: seeding creates a stub for each
// 대표페이지 Meta.Sites address without a 현장 page (address·거래처·특성 defaults,
// blank status), skips already-covered addresses, and is idempotent.
func TestSeedSitePages_BootstrapsUncoveredIdempotent(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	mustWrite(t, store, "프로젝트/군산수산리/대표.md", &Page{
		Meta: Frontmatter{
			Title: "군산 수산리 태양광", Type: "project", Client: "금호",
			Sites: []string{"전북 군산시 옥구읍 수산리", "전북 군산시 옥서면"},
			Kinds: []string{"태양광/토지"},
		},
		Body: "# 군산",
	})
	// 수산리 already has a 현장 page → should be skipped by the seeder.
	mustWrite(t, store, SitePagePath("군산수산리", "수산리"), &Page{
		Meta: Frontmatter{Title: "수산리", Type: "site", Address: "전북 군산시 옥구읍 수산리", Status: "계약"},
		Body: "현장.",
	})

	created, err := store.SeedSitePages("군산수산리")
	if err != nil {
		t.Fatalf("SeedSitePages: %v", err)
	}
	if len(created) != 1 || created[0] != SitePagePath("군산수산리", "옥서면") {
		t.Fatalf("created = %v, want only 프로젝트/군산수산리/현장/옥서면.md", created)
	}
	stub := testutil.Must(store.ReadPage(created[0]))
	if stub.Meta.Address != "전북 군산시 옥서면" || stub.Meta.Client != "금호" ||
		len(stub.Meta.Kinds) != 1 || stub.Meta.Kinds[0] != "태양광/토지" || stub.Meta.Status != "" {
		t.Errorf("stub meta = %+v, want address/거래처/특성 defaults + blank status", stub.Meta)
	}
	// The pre-existing 수산리 page must be untouched (still 계약).
	sur := testutil.Must(store.ReadPage(SitePagePath("군산수산리", "수산리")))
	if sur.Meta.Status != "계약" {
		t.Errorf("existing 수산리 page clobbered: %+v", sur.Meta)
	}
	// Idempotent: a second seed creates nothing.
	again, err := store.SeedSitePages("군산수산리")
	if err != nil || len(again) != 0 {
		t.Errorf("second seed = %v (err %v), want empty", again, err)
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
