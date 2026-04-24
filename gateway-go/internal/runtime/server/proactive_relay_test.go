package server

import "testing"

func TestSplitSessionKey(t *testing.T) {
	cases := []struct {
		name, key, wantCh, wantTgt string
		wantOK                     bool
	}{
		{"telegram ok", "telegram:7074071666", "telegram", "7074071666", true},
		{"empty", "", "", "", false},
		{"no colon", "telegram", "", "", false},
		{"empty channel", ":7074071666", "", "", false},
		{"empty target", "telegram:", "", "", false},
		{"channel only colon", ":", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, tgt, ok := splitSessionKey(tc.key)
			if ok != tc.wantOK || ch != tc.wantCh || tgt != tc.wantTgt {
				t.Fatalf("splitSessionKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.key, ch, tgt, ok, tc.wantCh, tc.wantTgt, tc.wantOK)
			}
		})
	}
}
