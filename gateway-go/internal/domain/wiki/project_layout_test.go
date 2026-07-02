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

// TestNormalizeProjectPagePath: flat project pages route onto the 대표.md slot;
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
