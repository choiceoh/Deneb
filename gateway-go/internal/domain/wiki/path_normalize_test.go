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
