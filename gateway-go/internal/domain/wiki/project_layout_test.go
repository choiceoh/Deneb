package wiki

import "testing"

// TestIsProjectRepPage locks the 대표페이지 rule for both layout forms.
func TestIsProjectRepPage(t *testing.T) {
	cases := map[string]bool{
		"프로젝트/영산고/대표.md":            true,  // in-folder form
		"프로젝트/영산고.md":               true,  // legacy flat form
		"프로젝트/영산고":                  false, // no .md — not a page path
		"프로젝트/영산고/로그.md":            false, // sub-page
		"프로젝트/영산고/기자재/케이블.md":       false,
		"프로젝트/영산고/메일분석/abc.md":      false,
		"프로젝트/메일분석/abc.md":          false, // category-level raw bucket
		"프로젝트/mail-analyses/abc.md": false,
		"프로젝트/거래/탑솔라.md":            false,
		"프로젝트/거래.md":                false, // reserved name as flat file — a bucket, not a project
		"프로젝트/거래/한빛/대표.md":          false, // rep slot inside a reserved bucket
		"인물/김민준.md":                 false,
		"프로젝트/":                     false,
	}
	for path, want := range cases {
		if got := IsProjectRepPage(path); got != want {
			t.Errorf("IsProjectRepPage(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestProjectNameOf covers name extraction across forms and reserved buckets.
func TestProjectNameOf(t *testing.T) {
	cases := []struct {
		path string
		name string
		ok   bool
	}{
		{"프로젝트/영산고/대표.md", "영산고", true},
		{"프로젝트/영산고/메일분석/abc.md", "영산고", true},
		{"프로젝트/영산고/기자재/케이블.md", "영산고", true},
		{"프로젝트/영산고.md", "영산고", true},
		{"프로젝트/메일분석/abc.md", "", false},
		{"프로젝트/mail-analyses/영산고/abc.md", "", false},
		{"프로젝트/거래/탑솔라.md", "", false},
		{"프로젝트/거래.md", "", false},
		{"업무/태양광.md", "", false},
	}
	for _, c := range cases {
		name, ok := ProjectNameOf(c.path)
		if name != c.name || ok != c.ok {
			t.Errorf("ProjectNameOf(%q) = (%q, %v), want (%q, %v)", c.path, name, ok, c.name, c.ok)
		}
	}
}

// TestNormalizeProjectPagePath: flat project pages route onto the 대표.md slot,
// overdeep paths (title-slash debris) fold into a single filename, and
// everything else is untouched.
func TestNormalizeProjectPagePath(t *testing.T) {
	cases := map[string]string{
		"프로젝트/영산고.md":       "프로젝트/영산고/대표.md",
		"프로젝트/영산고/대표.md":    "프로젝트/영산고/대표.md",
		"프로젝트/영산고/로그.md":    "프로젝트/영산고/로그.md",
		"프로젝트/영산고/사건-기록.md": "프로젝트/영산고/사건-기록.md",
		"프로젝트/거래.md":        "프로젝트/거래.md", // reserved bucket name stays put
		"프로젝트/메일분석/abc.md":  "프로젝트/메일분석/abc.md",
		"업무/태양광.md":         "업무/태양광.md",
		"인물/김민준.md":         "인물/김민준.md",
		// Canonical slot files stay put; deeper slot paths fold.
		"프로젝트/영산고/메일분석/19e8.md": "프로젝트/영산고/메일분석/19e8.md",
		"프로젝트/영산고/기자재/케이블.md":   "프로젝트/영산고/기자재/케이블.md",
		"프로젝트/영산고/메일분석/x/y.md":  "프로젝트/영산고/메일분석/x-y.md",
		"프로젝트/영산고/사건/세부/더세부.md": "프로젝트/영산고/사건-세부-더세부.md",
		// The real 2026-07-02 incident shape: a dreamer title with date slashes.
		"프로젝트/lg전자-재구매-—-6/25-회의-완료,-7/06-최종-결정-대기.md": "프로젝트/lg전자-재구매-—-6/25-회의-완료,-7-06-최종-결정-대기.md",
	}
	for in, want := range cases {
		if got := NormalizeProjectPagePath(in); got != want {
			t.Errorf("NormalizeProjectPagePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMailAnalysisPagePath: linked mails land in the project slot, unlinked in
// the category-level bucket.
func TestMailAnalysisPagePath(t *testing.T) {
	if got := MailAnalysisPagePath("영산고", "abc"); got != "프로젝트/영산고/메일분석/abc.md" {
		t.Errorf("linked = %q", got)
	}
	if got := MailAnalysisPagePath("", "abc"); got != "프로젝트/메일분석/abc.md" {
		t.Errorf("unlinked = %q", got)
	}
}

// TestIsProjectRawDataPath separates raw data from curated project content.
func TestIsProjectRawDataPath(t *testing.T) {
	cases := map[string]bool{
		"프로젝트/영산고/메일분석/abc.md":      true,
		"프로젝트/메일분석/abc.md":          true,
		"프로젝트/mail-analyses/abc.md": true,
		"프로젝트/거래/탑솔라.md":            true,
		"프로젝트/영산고/대표.md":            false,
		"프로젝트/영산고/로그.md":            false,
		"프로젝트/영산고/기자재/케이블.md":       false,
		"프로젝트/영산고.md":               false,
	}
	for path, want := range cases {
		if got := IsProjectRawDataPath(path); got != want {
			t.Errorf("IsProjectRawDataPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestProjectFolderOf: nested slots resolve to the owning project folder — the
// key the dreamer's code inheritance uses.
func TestProjectFolderOf(t *testing.T) {
	if folder, ok := ProjectFolderOf("프로젝트/영산고/메일분석/abc.md"); !ok || folder != "프로젝트/영산고" {
		t.Errorf("nested slot = (%q, %v)", folder, ok)
	}
	if folder, ok := ProjectFolderOf("프로젝트/영산고.md"); !ok || folder != "프로젝트/영산고" {
		t.Errorf("legacy flat = (%q, %v)", folder, ok)
	}
	if _, ok := ProjectFolderOf("프로젝트/거래/탑솔라.md"); ok {
		t.Error("reserved bucket must not resolve to a project folder")
	}
}

func TestProjectOfLinkedMailAnalysis(t *testing.T) {
	cases := map[string]struct {
		project string
		ok      bool
	}{
		"프로젝트/영산고/메일분석/msg123.md": {"영산고", true},
		"프로젝트/메일분석/msg123.md":     {"", false}, // unlinked bucket — no relationship
		"프로젝트/mail-analyses/a.md": {"", false}, // legacy bucket
		"프로젝트/거래/메일분석/msg.md":     {"", false}, // reserved dir is not a project
		"프로젝트/영산고/로그.md":          {"", false},
		"프로젝트/영산고/메일분석/깊은/msg.md": {"", false}, // extra depth
		"사람/김민준.md":               {"", false},
	}
	for path, want := range cases {
		project, ok := projectOfLinkedMailAnalysis(path)
		if project != want.project || ok != want.ok {
			t.Errorf("projectOfLinkedMailAnalysis(%q) = (%q, %v), want (%q, %v)", path, project, ok, want.project, want.ok)
		}
	}
}

// TestMaterialSlot locks the 자료 slot: paths, raw-data classification, the
// reserved global bucket, and overdeep folding under the slot.
func TestMaterialSlot(t *testing.T) {
	if got := MaterialPagePath("영산고", "발표-abcd1234.md"); got != "프로젝트/영산고/자료/발표-abcd1234.md" {
		t.Errorf("MaterialPagePath linked = %q", got)
	}
	if got := MaterialPagePath("", "발표-abcd1234.md"); got != "프로젝트/자료/발표-abcd1234.md" {
		t.Errorf("MaterialPagePath global = %q", got)
	}
	if !IsProjectRawDataPath("프로젝트/영산고/자료/x.md") || !IsProjectRawDataPath("프로젝트/자료/x.md") {
		t.Error("자료 pages must classify as raw data (excluded from curated dedup)")
	}
	if IsProjectRepPage("프로젝트/자료.md") {
		t.Error("reserved 자료 bucket flat file must not read as a project rep page")
	}
	if name, ok := ProjectNameOf("프로젝트/자료/x.md"); ok {
		t.Errorf("global 자료 bucket must not resolve to a project, got %q", name)
	}
	if name, ok := ProjectNameOf("프로젝트/영산고/자료/x.md"); !ok || name != "영산고" {
		t.Errorf("linked 자료 page must resolve to its project, got %q ok=%v", name, ok)
	}
	if got := NormalizeProjectPagePath("프로젝트/영산고/자료/a/b.md"); got != "프로젝트/영산고/자료/a-b.md" {
		t.Errorf("overdeep 자료 path must fold into the filename, got %q", got)
	}
	if !IsMaterialPath("프로젝트/영산고/자료/x.md") || !IsMaterialPath("프로젝트/자료/x.md") {
		t.Error("IsMaterialPath must match both 자료 buckets")
	}
	if IsMaterialPath("프로젝트/영산고/기자재/x.md") {
		t.Error("IsMaterialPath must not match other slots")
	}
}

// TestMeetingSlot locks the 회의록 slot: paths, raw-data classification, the
// reserved global bucket, and overdeep folding under the slot.
func TestMeetingSlot(t *testing.T) {
	if got := MeetingPagePath("영산고", "주간회의-abcd1234.md"); got != "프로젝트/영산고/회의록/주간회의-abcd1234.md" {
		t.Errorf("MeetingPagePath linked = %q", got)
	}
	if got := MeetingPagePath("", "주간회의-abcd1234.md"); got != "프로젝트/회의록/주간회의-abcd1234.md" {
		t.Errorf("MeetingPagePath global = %q", got)
	}
	if !IsProjectRawDataPath("프로젝트/영산고/회의록/x.md") || !IsProjectRawDataPath("프로젝트/회의록/x.md") {
		t.Error("회의록 pages must classify as raw data (excluded from curated dedup)")
	}
	if IsProjectRepPage("프로젝트/회의록.md") {
		t.Error("reserved 회의록 bucket flat file must not read as a project rep page")
	}
	if name, ok := ProjectNameOf("프로젝트/회의록/x.md"); ok {
		t.Errorf("global 회의록 bucket must not resolve to a project, got %q", name)
	}
	if name, ok := ProjectNameOf("프로젝트/영산고/회의록/x.md"); !ok || name != "영산고" {
		t.Errorf("linked 회의록 page must resolve to its project, got %q ok=%v", name, ok)
	}
	if got := NormalizeProjectPagePath("프로젝트/영산고/회의록/a/b.md"); got != "프로젝트/영산고/회의록/a-b.md" {
		t.Errorf("overdeep 회의록 path must fold into the filename, got %q", got)
	}
}
