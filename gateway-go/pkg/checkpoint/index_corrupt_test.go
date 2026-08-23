package checkpoint

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A torn index line (crash mid-write) must not hide the surviving snapshots —
// and must not hide itself either: the read reports how many lines it dropped.
func TestReadIndexSkipsCorruptLinesAndReportsThem(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	path := filepath.Join(t.TempDir(), "index.jsonl")
	content := `{"id":"a","seq":1}` + "\n" + `{"id":"b","seq":2` + "\n" + `not json at all` + "\n" + `{"id":"c","seq":3}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snaps, err := readIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[0].ID != "a" || snaps[1].ID != "c" {
		t.Fatalf("kept = %+v", snaps)
	}
	if out := buf.String(); !strings.Contains(out, "corruptLines=2") || !strings.Contains(out, "kept=2") {
		t.Fatalf("corrupt lines not reported: %q", out)
	}
}
