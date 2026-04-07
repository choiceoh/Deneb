package system

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestFindLatestLogFile(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "deneb-2025-01-01.log"), []byte("old"), 0o600)
	os.WriteFile(filepath.Join(dir, "deneb-2025-03-15.log"), []byte("mid"), 0o600)
	os.WriteFile(filepath.Join(dir, "deneb-2025-03-20.log"), []byte("new"), 0o600)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600)

	got := testutil.Must(findLatestLogFile(dir))
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

	got := testutil.Must(findLatestLogFile(dir))
	if filepath.Base(got) != "app.log" {
		t.Errorf("expected app.log, got %q", filepath.Base(got))
	}
}

func TestFindLatestLogFile_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "archive.log"), 0o700)
	os.WriteFile(filepath.Join(dir, "real.log"), []byte("data"), 0o600)

	got := testutil.Must(findLatestLogFile(dir))
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
