package wiki

import "testing"

// TestNormalizeWikiPath locks the source fix: a "w:" wikilink namespace the
// dreamer prefixes onto a path's category directory is stripped, so pages can't
// be filed under a phantom "w:프로젝트" bucket. Real paths pass through unchanged.
func TestNormalizeWikiPath(t *testing.T) {
	cases := map[string]string{
		"w:프로젝트/대한전선.md":             "프로젝트/대한전선.md",
		"w:운영시스템/mail-analyses/a.md": "운영시스템/mail-analyses/a.md",
		"  w:프로젝트/x.md  ":            "프로젝트/x.md",
		"프로젝트/영산고/b.md":              "프로젝트/영산고/b.md", // no namespace — unchanged
		"mail-analyses/c.md":         "mail-analyses/c.md",
		"":                           "",
	}
	for in, want := range cases {
		if got := normalizeWikiPath(in); got != want {
			t.Errorf("normalizeWikiPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeCategoryPath locks the 6-category taxonomy enforcement: a page is
// filed by its path's *directory* (the real bucket), so legacy dir names are
// remapped, the matching category is returned, and nothing lands at the root.
func TestNormalizeCategoryPath(t *testing.T) {
	cases := []struct {
		path, cat string
		wantPath  string
		wantCat   string
	}{
		// Valid category dir is kept; an empty/mismatched cat field is corrected to it.
		{"프로젝트/foo.md", "프로젝트", "프로젝트/foo.md", "프로젝트"},
		{"프로젝트/foo.md", "", "프로젝트/foo.md", "프로젝트"},
		{"기타/멜로니.md", "기타", "기타/멜로니.md", "기타"},               // 기타 is a real category, not just the default
		{"인물/홍길동.md", "프로젝트", "인물/홍길동.md", "인물"},             // valid dir wins over mismatched cat
		{"프로젝트/거래/탑솔라.md", "프로젝트", "프로젝트/거래/탑솔라.md", "프로젝트"}, // valid-cat sub-folder kept
		// Legacy dir names fold onto the taxonomy.
		{"거래/탑솔라.md", "거래", "프로젝트/탑솔라.md", "프로젝트"},
		{"결정/gemma.md", "결정", "프로젝트/gemma.md", "프로젝트"},
		{"기술/dgx.md", "기술", "업무/dgx.md", "업무"},
		{"사람/김부장.md", "", "인물/김부장.md", "인물"},
		{"선호/톤.md", "선호", "사용자/톤.md", "사용자"},
		{"운영시스템/ssh.md", "운영시스템", "시스템/ssh.md", "시스템"},
		// Unknown dir + unknown cat → 기타 catch-all.
		{"잡동사니/y.md", "몰라", "기타/y.md", "기타"},
		// No directory in the path: derive the bucket from the category field.
		{"foo.md", "프로젝트", "프로젝트/foo.md", "프로젝트"},
		{"foo.md", "거래", "프로젝트/foo.md", "프로젝트"},
		{"foo.md", "", "기타/foo.md", "기타"},
	}
	for _, c := range cases {
		gotPath, gotCat := normalizeCategoryPath(c.path, c.cat)
		if gotPath != c.wantPath || gotCat != c.wantCat {
			t.Errorf("normalizeCategoryPath(%q, %q) = (%q, %q), want (%q, %q)",
				c.path, c.cat, gotPath, gotCat, c.wantPath, c.wantCat)
		}
	}
}

// TestRemapLegacyCategory pins the legacy→taxonomy alias map and the no-mapping
// signal that routes unrecognized names to the catch-all.
func TestRemapLegacyCategory(t *testing.T) {
	mapped := map[string]string{
		"거래": "프로젝트", "결정": "프로젝트", "mail-analyses": "프로젝트",
		"사람": "인물", "기술": "업무", "선호": "사용자", "운영시스템": "시스템",
	}
	for in, want := range mapped {
		if got, ok := remapLegacyCategory(in); !ok || got != want {
			t.Errorf("remapLegacyCategory(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}
	// A current category is not "legacy" — it has no remapping (callers keep it as-is).
	for _, cur := range Categories {
		if _, ok := remapLegacyCategory(cur); ok {
			t.Errorf("remapLegacyCategory(%q) should report no mapping for a current category", cur)
		}
	}
	if _, ok := remapLegacyCategory("듣도보도못한것"); ok {
		t.Error("remapLegacyCategory should report no mapping for an unknown name")
	}
}
