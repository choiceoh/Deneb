package jsonlstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type record struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestLoadEmpty(t *testing.T) {
	items, err := Load[record](filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.jsonl")

	for i := 0; i < 3; i++ {
		if err := Append(path, record{Name: "item", Value: i}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := Load[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[2].Value != 2 {
		t.Fatalf("items[2].Value = %d, want 2", items[2].Value)
	}
}

func TestLoadMalformedRecordsPreservesValidEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.jsonl")
	data := `{"name":"a","value":1}
not json
{"name":"b","value":2}

{"name":"c","value":3}
{"name":"truncated"
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := Load[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (skipping malformed records)", len(items))
	}
}

func TestScanReportsSkippedLinesAndVisitsValidRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.jsonl")
	giant := strings.Repeat("x", maxLineBytes+1)
	data := `{"name":"a","value":1}` + "\n" +
		"not json\n" +
		`{"name":"` + giant + `","value":2}` + "\n" +
		`{"name":"c","value":3}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	var values []int
	stats, err := Scan[record](path, func(item record) {
		values = append(values, item.Value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Records != 2 || stats.CorruptLines != 1 || stats.OversizeLines != 1 || stats.SkippedLines() != 2 {
		t.Fatalf("unexpected scan stats: %+v", stats)
	}
	if len(values) != 2 || values[0] != 1 || values[1] != 3 {
		t.Fatalf("unexpected visited values: %v", values)
	}
}

func TestLoadOversizeLineIsSkippedNotFatal(t *testing.T) {
	// RSI 4th-review M2-#3: an oversize (>maxLineBytes) line — a torn write, a
	// merged record, or external corruption — must be skipped like any other
	// corrupt line, never abort the scan. The old bufio.Scanner returned
	// (partial, ErrTooLong), which the genesis held-out gate surfaced as an
	// engine error and, failing CLOSED, froze evolution for every skill sharing
	// the JSONL. Verify the good records survive and no error escapes.
	path := filepath.Join(t.TempDir(), "oversize.jsonl")
	giant := strings.Repeat("x", maxLineBytes+16)
	data := `{"name":"a","value":1}` + "\n" +
		`{"name":"` + giant + `","value":2}` + "\n" +
		`{"name":"c","value":3}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := Load[record](path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil (oversize line must skip, not fail)", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (oversize line skipped, neighbors kept)", len(items))
	}
	if items[0].Name != "a" || items[1].Name != "c" {
		t.Fatalf("unexpected surviving records: %+v", items)
	}
}

func TestLoadLineAtCeilingIsKept(t *testing.T) {
	// A line up to maxLineBytes is still valid — only strictly larger lines are
	// dropped, matching the prior scanner ceiling so nothing regresses.
	path := filepath.Join(t.TempDir(), "ceiling.jsonl")
	// Size Name so the marshaled line is exactly maxLineBytes. "y" never needs
	// JSON escaping (1 byte each) and Value 7 is a single digit, so the fixed
	// overhead is exactly the marshaling of a zero-length name with that value.
	overhead := len(`{"name":"","value":7}`)
	item := record{Name: strings.Repeat("y", maxLineBytes-overhead), Value: 7}
	if err := Append(path, item); err != nil {
		t.Fatal(err)
	}
	items, err := Load[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Value != 7 {
		t.Fatalf("near-ceiling line dropped: got %+v", items)
	}
}

func TestAppendMarshalErrorDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rejected.jsonl")
	item := struct {
		Callback func() `json:"callback"`
	}{Callback: func() {}}

	err := Append(path, item)

	if err == nil || !strings.Contains(err.Error(), "jsonlstore: marshal") {
		t.Fatalf("Append() error = %v, want wrapped marshal error", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("rejected append created file: stat error = %v", statErr)
	}
}

func TestSnapshotWritesAndReloadsItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.jsonl")
	items := []record{
		{Name: "x", Value: 10},
		{Name: "y", Value: 20},
	}

	if err := Snapshot(path, items); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load[record](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("got %d items, want 2", len(loaded))
	}
	if loaded[0].Name != "x" || loaded[1].Value != 20 {
		t.Fatalf("unexpected data: %+v", loaded)
	}
}

var benchmarkRecordCount int

func benchmarkJSONLPath(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "records.jsonl")
	items := make([]record, 10_000)
	for i := range items {
		items[i] = record{Name: "benchmark-record", Value: i}
	}
	if err := Snapshot(path, items); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkLoad10K(b *testing.B) {
	path := benchmarkJSONLPath(b)
	b.ResetTimer()
	for range b.N {
		items, err := Load[record](path)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRecordCount = len(items)
	}
}

func BenchmarkScan10K(b *testing.B) {
	path := benchmarkJSONLPath(b)
	b.ResetTimer()
	for range b.N {
		count := 0
		_, err := Scan[record](path, func(_ record) { count++ })
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRecordCount = count
	}
}
