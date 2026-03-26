package rpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindLatestLogFile(t *testing.T) {
	dir := t.TempDir()

	// Create some log files.
	os.WriteFile(filepath.Join(dir, "deneb-2025-01-01.log"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(dir, "deneb-2025-03-15.log"), []byte("mid"), 0o600)
	os.WriteFile(filepath.Join(dir, "deneb-2025-03-20.log"), []byte("new"), 0o600)
	// Non-log file should be ignored.
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600)

	got, err := findLatestLogFile(dir)
	if err != nil {
		t.Fatalf("findLatestLogFile() error: %v", err)
	}
	want := filepath.Join(dir, "deneb-2025-03-20.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindLatestLogFile_NoLogFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte("x"), 0o600)

	_, err := findLatestLogFile(dir)
	if err == nil {
		t.Error("expected error when no log files exist")
	}
}

func TestFindLatestLogFile_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := findLatestLogFile(dir)
	if err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestFindLatestLogFile_SingleFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.log"), []byte("single"), 0o600)

	got, err := findLatestLogFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(got) != "app.log" {
		t.Errorf("expected app.log, got %q", filepath.Base(got))
	}
}

func TestFindLatestLogFile_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	// Create a directory ending in .log — should be skipped.
	os.MkdirAll(filepath.Join(dir, "archive.log"), 0o700)
	os.WriteFile(filepath.Join(dir, "real.log"), []byte("data"), 0o600)

	got, err := findLatestLogFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(got) != "real.log" {
		t.Errorf("expected real.log, got %q", filepath.Base(got))
	}
}

func TestTruncateLog(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "12345", 5, "12345"},
		{"truncated", "1234567890", 5, "12345\n... (truncated)"},
		{"empty", "", 10, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateLog(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("truncateLog(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

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
	// Nil params.
	err := unmarshalParams(nil, &struct{}{})
	if err == nil {
		t.Error("expected error for nil params")
	}

	// Empty params.
	err = unmarshalParams([]byte{}, &struct{}{})
	if err == nil {
		t.Error("expected error for empty params")
	}

	// Valid JSON.
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

	// Invalid JSON.
	err = unmarshalParams([]byte(`{invalid`), &out)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
