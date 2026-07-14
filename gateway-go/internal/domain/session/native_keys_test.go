package session

import "testing"

func TestRestorableTranscriptChannelParsesValidAndInvalidKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key string
		ok  bool
	}{
		{key: "client:main", ok: true},
		{key: "client:main:project", ok: true},
		{key: "chat:legacy", ok: true},
		{key: "client:topic:old", ok: false},
		{key: "client:uuid", ok: false},
		{key: "chat:", ok: false},
		{key: "", ok: false},
	} {
		channel, ok := RestorableTranscriptChannel(tc.key)
		if ok != tc.ok {
			t.Errorf("RestorableTranscriptChannel(%q) ok = %v, want %v", tc.key, ok, tc.ok)
		}
		if ok && channel != "client" {
			t.Errorf("RestorableTranscriptChannel(%q) channel = %q, want client", tc.key, channel)
		}
	}
}
