package server

import "testing"

// TestRestorableTranscriptSession pins the startup-restore filter to the live
// native session shapes only. Retired keys — the topic sessions removed in
// #1963 and the pre-main client:<uuid> format — must NOT resurrect: they linger
// on disk as transcript files but kept zombie-reviving the drawer on every
// SIGUSR1 restart because the filter used to match bare isNativeClientSessionKey.
func TestRestorableTranscriptSession(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"client:main", true}, // the single home session
		{"client:main:0a17341a-d6e2-4686-aed5-b9ce9841cc68", true}, // explicit new conversation
		{"client:topic:업무", false},                                 // retired topic session (#1963)
		{"client:topic:업무:64c14b8b-c9e6-40ed-a807-60eb882fd8c2", false},
		{"client:topic:잡담", false},
		{"client:6ae56098-122c-40ff-a5bd-c9e6cad6faa8", false}, // pre-main legacy format
		{"client:", false},
		{"client:mainx", false},        // neither client:main nor client:main:*
		{"telegram:7074071666", false}, // non-native channel
		{"", false},
	}
	for _, c := range cases {
		ch, ok := restorableTranscriptSession(c.key)
		if ok != c.want {
			t.Errorf("restorableTranscriptSession(%q) ok=%v, want %v", c.key, ok, c.want)
		}
		if ok && ch != "client" {
			t.Errorf("restorableTranscriptSession(%q) channel=%q, want \"client\"", c.key, ch)
		}
	}
}

// TestRetiredKeysStillNativeButNotRestorable documents the deliberate gap: the
// retired keys remain "native client" keys for activity/heartbeat/resume
// purposes (isNativeClientSessionKey stays broad), yet must not be restored.
func TestRetiredKeysStillNativeButNotRestorable(t *testing.T) {
	retired := []string{
		"client:topic:업무",
		"client:6ae56098-122c-40ff-a5bd-c9e6cad6faa8",
	}
	for _, k := range retired {
		if !isNativeClientSessionKey(k) {
			t.Errorf("isNativeClientSessionKey(%q) = false; retired keys should still count as native", k)
		}
		if isRestorableNativeSessionKey(k) {
			t.Errorf("isRestorableNativeSessionKey(%q) = true; retired keys must not be restorable", k)
		}
	}
}
