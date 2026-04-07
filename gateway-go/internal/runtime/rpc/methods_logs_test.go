package rpc

import (
	"testing"
)

func TestTruncateForError(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"short", "hello", "hello"},
		{"exact boundary", string(make([]byte, maxKeyInErrorMsg)), string(make([]byte, maxKeyInErrorMsg))},
		{"over boundary", string(make([]byte, maxKeyInErrorMsg+10)), string(make([]byte, maxKeyInErrorMsg)) + "..."},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateForError(tc.input)
			if got != tc.want {
				t.Errorf("truncateForError() len=%d, want len=%d", len(got), len(tc.want))
			}
		})
	}
}

func TestUnmarshalParams(t *testing.T) {
	err := unmarshalParams(nil, &struct{}{})
	if err == nil {
		t.Error("expected error for nil params")
	}

	err = unmarshalParams([]byte{}, &struct{}{})
	if err == nil {
		t.Error("expected error for empty params")
	}

	var out struct {
		Name string `json:"name"`
	}
	err = unmarshalParams([]byte(`{"name":"test"}`), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "test" {
		t.Errorf("expected name 'test', got %q", out.Name)
	}

	err = unmarshalParams([]byte(`{invalid`), &out)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
