package server

import "testing"

// Caller hints must outrank the wiki-wide list: a per-request bias exists
// because the caller knows something the index does not.
func TestMergeHotwordListsPutsCallerHintsFirst(t *testing.T) {
	got := mergeHotwordLists("곡성, 금호타이어", "대한전선, 에코프로")
	want := "곡성, 금호타이어, 대한전선, 에코프로"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeHotwordListsDropsBlanksAndDuplicates(t *testing.T) {
	// The wiki list is machine-built and the caller list is hand-written, so
	// overlap is the normal case, not an edge one — a bias list that repeats a
	// term wastes the budget the sidecar caps.
	got := mergeHotwordLists(" 곡성 ,, 금호타이어 ", "금호타이어, , 곡성, 대한전선")
	want := "곡성, 금호타이어, 대한전선"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeHotwordListsHandlesEmptySides(t *testing.T) {
	if got := mergeHotwordLists("", "대한전선"); got != "대한전선" {
		t.Errorf("empty caller side: got %q", got)
	}
	if got := mergeHotwordLists("곡성", ""); got != "곡성" {
		t.Errorf("empty wiki side: got %q", got)
	}
	if got := mergeHotwordLists("", ""); got != "" {
		t.Errorf("both empty: got %q", got)
	}
}
