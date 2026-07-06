package server

import "testing"

func TestSanitizeMCPToolName(t *testing.T) {
	cases := map[string]string{
		"search_recordings": "search_recordings",
		"get-transcript":    "get-transcript",
		"weird.name/v2":     "weird_name_v2",
		"한글이름":              "____",
	}
	for in, want := range cases {
		if got := sanitizeMCPToolName(in); got != want {
			t.Errorf("sanitizeMCPToolName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeMCPToolName(""); got == "" {
		t.Error("empty name must map to a non-empty fallback")
	}
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	if got := sanitizeMCPToolName(string(long)); len(got) > 64 {
		t.Errorf("long name not truncated: %d chars", len(got))
	}
}
