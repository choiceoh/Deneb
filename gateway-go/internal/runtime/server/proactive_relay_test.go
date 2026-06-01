package server

import "testing"

func TestResolveHome(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		activeHome func() int64
		wantKey    string
		wantOK     bool
	}{
		{"non-sentinel passthrough", "telegram:-1003946703971", nil, "telegram:-1003946703971", true},
		{"non-sentinel with thread passthrough", "telegram:-1003946703971:thread:5", nil, "telegram:-1003946703971:thread:5", true},
		{"sentinel resolves to active home", homeSessionKey, func() int64 { return -1003946703971 }, "telegram:-1003946703971", true},
		{"sentinel unresolved (activeHome returns 0)", homeSessionKey, func() int64 { return 0 }, "", false},
		{"sentinel unresolved (no resolver)", homeSessionKey, nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := proactiveRelayDeps{activeHome: tc.activeHome}
			gotKey, gotOK := d.resolveHome(tc.key)
			if gotKey != tc.wantKey || gotOK != tc.wantOK {
				t.Fatalf("resolveHome(%q) = (%q, %v), want (%q, %v)",
					tc.key, gotKey, gotOK, tc.wantKey, tc.wantOK)
			}
		})
	}
}

func TestMirrorsToNativeWork(t *testing.T) {
	cases := []struct {
		name, channel, target string
		want                  bool
	}{
		{"telegram general (no thread)", "telegram", "-1003946703971", true},
		{"telegram general (thread 0)", "telegram", "-1003946703971:thread:0", true},
		{"telegram named topic", "telegram", "-1003946703971:thread:5", false},
		{"non-telegram channel", "client", "main", false},
		{"empty channel", "", "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mirrorsToNativeWork(tc.channel, tc.target); got != tc.want {
				t.Fatalf("mirrorsToNativeWork(%q,%q) = %v, want %v", tc.channel, tc.target, got, tc.want)
			}
		})
	}
}

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
