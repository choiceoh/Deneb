package codeaction

import (
	"strings"
	"testing"
)

// The surface must come from the implementation, not a copy of it. This test is
// the reason the copy was deleted: the first hardcoded list already omitted
// `gmail` on the day it was written.
func TestBridgeSurfaceMatchesTheEmbeddedRuntime(t *testing.T) {
	got := BridgeSurface()
	want := []string{"calendar", "contacts", "deals", "edit", "gmail", "mail_archive", "read", "wiki", "write"}
	if len(got) != len(want) {
		t.Fatalf("surface = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("surface = %v, want %v (sorted)", got, want)
		}
	}
	// Private helpers stay out — `_call` is the bridge's own transport.
	for _, n := range got {
		if strings.HasPrefix(n, "_") {
			t.Errorf("private method %q leaked into the surface", n)
		}
	}
}

// Every name the surface claims must really be a method on the class, so a
// rename in the runtime cannot leave the guard pointing at a ghost.
func TestBridgeSurfaceNamesExistInRuntime(t *testing.T) {
	for _, n := range BridgeSurface() {
		if !strings.Contains(codeActionRuntime, "def "+n+"(") {
			t.Errorf("surface names %q but the runtime has no such method", n)
		}
	}
}
