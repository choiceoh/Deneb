package runtimeops

import (
	"testing"
)

func TestBoundaryFleetFormattingHelpers(t *testing.T) {
	if fleetDash("") != "—" || fleetDash("node") != "node" {
		t.Fatal("fleetDash contract changed")
	}
	value := 7
	if fleetIntp(nil) != "—" || fleetIntp(&value) != "7" {
		t.Fatal("fleetIntp contract changed")
	}
	for kb, want := range map[int64]string{
		0:                "0",
		1024 * 1024:      "1",
		1536 * 1024:      "2",
		10 * 1024 * 1024: "10",
	} {
		if got := fleetGiB(kb); got != want {
			t.Fatalf("fleetGiB(%d) = %q, want %q", kb, got, want)
		}
	}
}
