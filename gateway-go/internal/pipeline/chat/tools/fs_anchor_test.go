package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLineAnchorHash_Deterministic(t *testing.T) {
	a := lineAnchorHash("func main() {")
	b := lineAnchorHash("func main() {")
	if a != b {
		t.Fatalf("hash not deterministic: %s != %s", a, b)
	}
	if len(a) != 6 {
		t.Fatalf("expected 6 hex chars, got %q", a)
	}
	if lineAnchorHash("alpha") == lineAnchorHash("beta") {
		t.Fatal("distinct lines collided")
	}
}

func TestEditByAnchor_SingleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	anchor := lineAnchorHash("beta")
	out, err := editByAnchor(path, "f.txt", content, anchor, "", "BETA")
	if err != nil {
		t.Fatalf("editByAnchor: %v", err)
	}
	if !strings.Contains(out, "anchor line 2") {
		t.Errorf("unexpected result message: %q", out)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "alpha\nBETA\ngamma\n" {
		t.Errorf("got %q", string(got))
	}
}

func TestEditByAnchor_Range(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "a\nb\nc\nd\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	start := lineAnchorHash("b")
	end := lineAnchorHash("c")
	if _, err := editByAnchor(path, "f.txt", content, start, end, "X\nY\nZ"); err != nil {
		t.Fatalf("editByAnchor range: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nX\nY\nZ\nd\n" {
		t.Errorf("got %q", string(got))
	}
}

func TestEditByAnchor_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "x\nsame\nsame\ny\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	anchor := lineAnchorHash("same")
	if _, err := editByAnchor(path, "f.txt", content, anchor, "", "Z"); err == nil {
		t.Fatal("expected ambiguous anchor error")
	}
}

func TestEditByAnchor_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "x\ny\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := editByAnchor(path, "f.txt", content, "ffffff", "", "Z"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestToolRead_HashesColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in, _ := json.Marshal(map[string]any{"file_path": path, "hashes": true})
	out, err := ToolRead(dir)(context.Background(), in)
	if err != nil {
		t.Fatalf("ToolRead: %v", err)
	}

	want := lineAnchorHash("hello")
	if !strings.Contains(out, "1\t"+want+"\thello") {
		t.Errorf("expected hash column for line 1 (anchor %s), got:\n%s", want, out)
	}
}

func TestToolEdit_AnchorEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"new_string": "TWO",
		"anchor":     lineAnchorHash("two"),
	})
	if _, err := ToolEdit(dir)(context.Background(), in); err != nil {
		t.Fatalf("ToolEdit anchor: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\nTWO\nthree\n" {
		t.Errorf("got %q", string(got))
	}
}
