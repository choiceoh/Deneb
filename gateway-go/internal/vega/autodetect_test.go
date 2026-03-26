package vega

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoDetectModels_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	models := AutoDetectModels(dir)
	if len(models) != 0 {
		t.Errorf("expected 0 models in empty dir, got %d", len(models))
	}
}

func TestAutoDetectModels_EmptyString(t *testing.T) {
	models := AutoDetectModels("")
	if models != nil {
		t.Errorf("expected nil for empty path, got %v", models)
	}
}

func TestAutoDetectModels_NonexistentDir(t *testing.T) {
	models := AutoDetectModels("/nonexistent/path/that/does/not/exist")
	if models != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", models)
	}
}

func TestAutoDetectModels_DetectsGGUF(t *testing.T) {
	dir := t.TempDir()

	// Create .gguf files.
	for _, name := range []string{"embedder.gguf", "reranker.GGUF", "model.Gguf"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fake-gguf"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create non-gguf files (should be ignored).
	for _, name := range []string{"readme.md", "config.json", "model.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not-a-model"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a subdirectory (should be skipped).
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	models := AutoDetectModels(dir)
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d: %+v", len(models), models)
	}

	for _, m := range models {
		if m.Name == "" {
			t.Error("model Name should not be empty")
		}
		if m.Path == "" {
			t.Error("model Path should not be empty")
		}
		if m.Size != int64(len("fake-gguf")) {
			t.Errorf("model %s Size = %d, want %d", m.Name, m.Size, len("fake-gguf"))
		}
	}
}

func TestAutoDetectModels_PathIsCorrect(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.gguf"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	models := AutoDetectModels(dir)
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	want := filepath.Join(dir, "test.gguf")
	if models[0].Path != want {
		t.Errorf("Path = %q, want %q", models[0].Path, want)
	}
}

func TestDefaultModelDir(t *testing.T) {
	dir := DefaultModelDir()
	// Should end with .deneb/models.
	if filepath.Base(dir) != "models" {
		t.Errorf("expected dir to end with 'models', got %q", dir)
	}
	parent := filepath.Base(filepath.Dir(dir))
	if parent != ".deneb" {
		t.Errorf("expected parent to be '.deneb', got %q", parent)
	}
}

func TestShouldEnableVega_NoFFI(t *testing.T) {
	got := ShouldEnableVega(false, "/any/dir", nil)
	if got {
		t.Error("should return false when FFI is not available")
	}
}

func TestShouldEnableVega_FFIWithModels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.gguf"), []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}

	got := ShouldEnableVega(true, dir, nil)
	if !got {
		t.Error("should return true when FFI available and models found")
	}
}

func TestShouldEnableVega_FFINoModels_FTSOnly(t *testing.T) {
	dir := t.TempDir() // Empty dir, no .gguf files.

	got := ShouldEnableVega(true, dir, nil)
	if !got {
		t.Error("should return true for FTS-only mode when FFI available")
	}
}
