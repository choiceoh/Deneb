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

func TestSnapshot(t *testing.T) {
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
