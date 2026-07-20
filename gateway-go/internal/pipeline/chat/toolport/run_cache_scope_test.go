package toolport

import "testing"

// Mixed path spellings between the recorder (search scope) and the
// invalidator (mutation file_path) must fail toward invalidation — the raw
// model-provided paths are never normalized through a shared root, so an
// absolute-scoped entry surviving a relative-path edit (or vice versa) would
// replay stale search results within the run.
func TestInvalidateByPathMixedSpellingIsConservative(t *testing.T) {
	cases := []struct {
		name    string
		scope   string
		mutPath string
	}{
		{"absolute scope, relative mutation", "/home/user/ws/docs", "docs/notes.txt"},
		{"relative scope, absolute mutation", "docs", "/home/user/ws/docs/notes.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := NewRunCache()
			rc.SetWithScope("grep:q", "cached", tc.scope)
			rc.InvalidateByPath(tc.mutPath)
			if _, ok := rc.Get("grep:q"); ok {
				t.Fatal("mixed-spelling scope must invalidate conservatively")
			}
		})
	}
}

// Same-spelling scopes keep the selective behavior: an unrelated sibling
// directory survives, the containing scope is dropped.
func TestInvalidateByPathSameSpellingStaysSelective(t *testing.T) {
	rc := NewRunCache()
	rc.SetWithScope("grep:in-scope", "cached", "docs")
	rc.SetWithScope("grep:sibling", "cached", "src")
	rc.InvalidateByPath("docs/notes.txt")
	if _, ok := rc.Get("grep:in-scope"); ok {
		t.Fatal("in-scope entry must be invalidated")
	}
	if _, ok := rc.Get("grep:sibling"); !ok {
		t.Fatal("unrelated sibling scope must survive")
	}
}
