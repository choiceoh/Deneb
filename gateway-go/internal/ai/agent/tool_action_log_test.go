package agent

import "testing"

// TestToolCallActionAttributesMultiplexedTools pins the log attribution added
// 2026-08-30. 29 registered tools multiplex on one field (26 on `action`, plus
// knowledge's `op` and phone_read's `what`), and the tool-complete line recorded
// none of it — the log could say wiki took 19s without saying whether that was a
// search, a write, or an index rebuild.
func TestToolCallActionAttributesMultiplexedTools(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"action", `{"action":"search","query":"금호타이어"}`, "search"},
		{"op (knowledge)", `{"op":"recall","query":"x"}`, "recall"},
		{"what (phone_read)", `{"what":"battery"}`, "battery"},
		{"action wins over op", `{"action":"write","op":"ignored"}`, "write"},
		{"single-purpose tool", `{"file_path":"/tmp/a.go"}`, ""},
		{"empty input", ``, ""},
		{"malformed input", `{"action":`, ""},
		{"blank action", `{"action":"   "}`, ""},
		// exec's `command` is deliberately not read: a whole shell line has no
		// place in a structured field, and it can carry a path or a credential.
		{"exec command is not an action", `{"command":"cat ~/.deneb/credentials/keys.env"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolCallAction([]byte(tc.input)); got != tc.want {
				t.Fatalf("toolCallAction(%s) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestToolCallActionClipsAStrayLongValue keeps a free-text field that happens to
// be named `action` from dumping a paragraph into every log line.
func TestToolCallActionClipsAStrayLongValue(t *testing.T) {
	long := `{"action":"` + string(make([]byte, 0, 100)) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"}`
	got := toolCallAction([]byte(long))
	if len(got) != 32 {
		t.Fatalf("len = %d, want the 32-char clip: %q", len(got), got)
	}
}
