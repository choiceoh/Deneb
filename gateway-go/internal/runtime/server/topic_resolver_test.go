package server

import "testing"

// newTestResolver builds a topicResolver directly (newTopicResolver reads
// deneb.json, which isn't present in tests) so we can exercise the threadID→key
// map in isolation.
func newTestResolver(m map[string]string) *topicResolver {
	fwd := make(map[string]string, len(m))
	for threadID, key := range m {
		fwd[threadID] = key
	}
	return &topicResolver{dir: "topics", m: fwd}
}

func TestTopicResolver_TopicKey(t *testing.T) {
	r := newTestResolver(map[string]string{"0": "업무", "42": "코딩", "57": "잡담"})

	cases := []struct {
		threadID string
		want     string
	}{
		{"0", "업무"},
		{"42", "코딩"},
		{"57", "잡담"},
		{"", "업무"},  // empty threadID normalizes to "0" (General)
		{"999", ""}, // unmapped threadID → no injection
	}
	for _, c := range cases {
		if got := r.TopicKey(c.threadID); got != c.want {
			t.Errorf("TopicKey(%q) = %q, want %q", c.threadID, got, c.want)
		}
	}
}
