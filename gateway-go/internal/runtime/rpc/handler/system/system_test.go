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
