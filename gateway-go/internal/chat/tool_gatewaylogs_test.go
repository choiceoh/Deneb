package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilterByLevel(t *testing.T) {
	lines := []string{
		"14:05:09.1 DBG │ [server] debug message",
		"14:05:09.2 INF │ [server] info message",
		"14:05:09.3 WRN │ [chat] warning message",
		"14:05:09.4 ERR │ [chat] error message",
	}

	tests := []struct {
		level string
		want  int
	}{
		{"debug", 4},
		{"info", 3},
		{"warn", 2},
		{"error", 1},
	}

	for _, tt := range tests {
		got := filterByLevel(lines, tt.level)
		if len(got) != tt.want {
			t.Errorf("filterByLevel(%q) = %d lines, want %d", tt.level, len(got), tt.want)
		}
	}
}

func TestExtractLevelRank(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"14:05:09.1 DBG │ [server] debug msg", 0},
		{"14:05:09.1 INF │ [server] info msg", 1},
		{"14:05:09.1 WRN │ [chat] warn msg", 2},
		{"14:05:09.1 ERR │ [chat] error msg", 3},
		{"short", -1},
	}

	for _, tt := range tests {
		got := extractLevelRank(tt.line)
		if got != tt.want {
			t.Errorf("extractLevelRank(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	input := "\033[2m14:05:09.1\033[0m \033[1;34mINF\033[0m \033[2m│\033[0m \033[2;36m[server]\033[0m \033[1mtest\033[0m"
	got := stripANSI(input)
	want := "14:05:09.1 INF │ [server] test"
	if got != want {
		t.Errorf("stripANSI:\n got: %q\nwant: %q", got, want)
	}
}

func TestExtractLevelRank_WithANSI(t *testing.T) {
	line := "\033[2m14:05:09.1\033[0m \033[1;31mERR\033[0m \033[2m│\033[0m \033[2;36m[chat]\033[0m \033[1;31msomething broke\033[0m"
	got := extractLevelRank(line)
	if got != 3 {
		t.Errorf("extractLevelRank with ANSI ERR = %d, want 3", got)
	}
}

func TestReadTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	var content string
	for i := 1; i <= 20; i++ {
		content += "line\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := readTailLines(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Errorf("readTailLines(5) = %d lines, want 5", len(lines))
	}
}

func TestReadTailLines_FileNotFound(t *testing.T) {
	_, err := readTailLines("/nonexistent/path/file.log", 10)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
