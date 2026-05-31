package server

import "testing"

// newTestResolver builds a topicResolver directly (newTopicResolver reads
// deneb.json, which isn't present in tests) so we can exercise the forward and
// reverse maps in isolation.
func newTestResolver(m map[string]string) *topicResolver {
	fwd := make(map[string]string, len(m))
	rev := make(map[string]string, len(m))
	for threadID, key := range m {
		fwd[threadID] = key
		rev[key] = threadID
	}
	return &topicResolver{dir: "topics", m: fwd, rev: rev}
}

func TestTopicResolver_ThreadIDForKey_RoundTrips(t *testing.T) {
	r := newTestResolver(map[string]string{"0": "업무", "42": "코딩", "57": "잡담"})

	cases := []struct {
		key     string
		wantTID string
		wantOK  bool
	}{
		{"업무", "0", true},
		{"코딩", "42", true},
		{"잡담", "57", true},
		{"없는키", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotTID, gotOK := r.ThreadIDForKey(c.key)
		if gotTID != c.wantTID || gotOK != c.wantOK {
			t.Errorf("ThreadIDForKey(%q) = (%q, %v), want (%q, %v)", c.key, gotTID, gotOK, c.wantTID, c.wantOK)
		}
		// Reverse must round-trip back through TopicKey for found keys.
		if c.wantOK && r.TopicKey(gotTID) != c.key {
			t.Errorf("round-trip: TopicKey(ThreadIDForKey(%q)) = %q, want %q", c.key, r.TopicKey(gotTID), c.key)
		}
	}
}

func TestTopicResolver_ThreadIDForKey_UnknownKeyDoesNotResolveToGeneral(t *testing.T) {
	// A General entry exists, but an unknown key must report ok=false rather
	// than falling through to General — otherwise a stale client topic key
	// would silently inject the wrong knowledge.
	r := newTestResolver(map[string]string{"0": "업무", "42": "코딩"})
	if tid, ok := r.ThreadIDForKey("stale-topic"); ok || tid != "" {
		t.Errorf("unknown key resolved to (%q, %v), want (\"\", false)", tid, ok)
	}
}
