package jsonutil

import "testing"

func TestEscapeStringControls(t *testing.T) {
	// Valid JSON (no raw controls) is unchanged.
	valid := `{"a":"b","c":[1,2]}`
	if got := EscapeStringControls(valid); got != valid {
		t.Errorf("valid JSON altered: %q", got)
	}
	// A raw newline inside a string literal is escaped; structural whitespace
	// (the newline between tokens) is left alone.
	in := "{\n  \"reason\": \"line1\nline2\"\n}"
	got := EscapeStringControls(in)
	if got == in {
		t.Fatal("expected the in-string newline to be escaped")
	}
	// The escaped result must parse and round-trip the multi-line value.
	type r struct {
		Reason string `json:"reason"`
	}
	v, err := UnmarshalLLM[r](in)
	if err != nil {
		t.Fatalf("UnmarshalLLM rejected an unescaped-newline object: %v", err)
	}
	if v.Reason != "line1\nline2" {
		t.Errorf("reason = %q, want line1\\nline2", v.Reason)
	}
}

type misRow struct {
	Path    string `json:"path"`
	Correct string `json:"correctCategory"`
	Reason  string `json:"reason"`
}

func TestUnmarshalLLMArray(t *testing.T) {
	cases := map[string]string{
		"plain":            `[{"path":"a.md","correctCategory":"업무","reason":"ok"}]`,
		"fenced":           "```json\n[{\"path\":\"a.md\",\"correctCategory\":\"업무\",\"reason\":\"ok\"}]\n```",
		"thinking preface": "<think>let me check</think>\n[{\"path\":\"a.md\",\"correctCategory\":\"업무\",\"reason\":\"ok\"}]",
		"trailing comma":   `[{"path":"a.md","correctCategory":"업무","reason":"ok"},]`,
		// The real wiki-verify failure: an unescaped newline in the Korean reason.
		"raw newline in string": "[{\"path\":\"a.md\",\"correctCategory\":\"업무\",\"reason\":\"제공된 카테고리\n기준\"}]",
		// Trailing prose after the array.
		"trailing prose": `[{"path":"a.md","correctCategory":"업무","reason":"ok"}] 끝입니다.`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			rows, err := UnmarshalLLMArray[misRow](raw)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if len(rows) != 1 || rows[0].Path != "a.md" || rows[0].Correct != "업무" {
				t.Fatalf("unexpected rows: %+v", rows)
			}
		})
	}

	// Empty array (the "no problems" case) parses to an empty slice, not an error.
	if rows, err := UnmarshalLLMArray[misRow]("[]"); err != nil || len(rows) != 0 {
		t.Errorf("empty array: rows=%+v err=%v", rows, err)
	}
}

func TestUnmarshalLLMArray_Truncated(t *testing.T) {
	// Token-limit truncation mid-second-object: recover the first complete one.
	raw := `[{"path":"a.md","correctCategory":"업무","reason":"ok"},{"path":"b.md","correctCateg`
	rows, err := UnmarshalLLMArray[misRow](raw)
	if err != nil {
		t.Fatalf("truncation recovery failed: %v", err)
	}
	if len(rows) != 1 || rows[0].Path != "a.md" {
		t.Errorf("recovered rows = %+v, want the one complete element", rows)
	}
}
