package wiki

import "testing"

// TestNormalizeCategory locks the category cleanup: a wikilink namespace (w:)
// or bracket form collapses to the plain name so the browser doesn't split one
// bucket into phantom categories, while intentional path sub-buckets are kept.
func TestNormalizeCategory(t *testing.T) {
	cases := map[string]string{
		"프로젝트":                  "프로젝트",                // plain — unchanged
		"w:프로젝트":                "프로젝트",                // wikilink namespace stripped
		"  w:프로젝트  ":            "프로젝트",                // surrounding space trimmed
		"[[프로젝트]]":              "프로젝트",                // bracket form
		"[[w:프로젝트]]":            "프로젝트",                // bracket + namespace
		"프로젝트/영산고":              "프로젝트/영산고",            // path kept (sub-bucket)
		"w:운영시스템/mail-analyses": "운영시스템/mail-analyses", // w: stripped, path kept
		"":                      "",
	}
	for in, want := range cases {
		if got := normalizeCategory(in); got != want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
