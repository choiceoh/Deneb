package autoresearch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractConstants(t *testing.T) {
	dir := t.TempDir()
	content := "lr = 0.001\nbatch_size = 32\n"
	if err := os.WriteFile(filepath.Join(dir, "train.py"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	constants := []ConstantDef{
		{Name: "LEARNING_RATE", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
		{Name: "BATCH_SIZE", File: "train.py", Pattern: `batch_size\s*=\s*(\d+)`, Type: "int"},
	}
	vals, err := ExtractConstants(dir, constants)
	if err != nil {
		t.Fatal(err)
	}
	if vals["LEARNING_RATE"] != "0.001" {
		t.Errorf("LEARNING_RATE = %q, want %q", vals["LEARNING_RATE"], "0.001")
	}
	if vals["BATCH_SIZE"] != "32" {
		t.Errorf("BATCH_SIZE = %q, want %q", vals["BATCH_SIZE"], "32")
	}
}

func TestExtractConstantsMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.py"), []byte("y = 20\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	constants := []ConstantDef{
		{Name: "X", File: "a.py", Pattern: `x\s*=\s*(\d+)`, Type: "int"},
		{Name: "Y", File: "b.py", Pattern: `y\s*=\s*(\d+)`, Type: "int"},
	}
	vals, err := ExtractConstants(dir, constants)
	if err != nil {
		t.Fatal(err)
	}
	if vals["X"] != "10" {
		t.Errorf("X = %q, want %q", vals["X"], "10")
	}
	if vals["Y"] != "20" {
		t.Errorf("Y = %q, want %q", vals["Y"], "20")
	}
}

func TestExtractConstantsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "train.py"), []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	constants := []ConstantDef{
		{Name: "MISSING", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
	}
	_, err := ExtractConstants(dir, constants)
	if err == nil {
		t.Fatal("expected error for missing constant match")
	}
}

func TestApplyOverrides(t *testing.T) {
	dir := t.TempDir()
	original := "lr = 0.001  # learning rate\nbatch_size = 32\n"
	if err := os.WriteFile(filepath.Join(dir, "train.py"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	constants := []ConstantDef{
		{Name: "LEARNING_RATE", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
		{Name: "BATCH_SIZE", File: "train.py", Pattern: `batch_size\s*=\s*(\d+)`, Type: "int"},
	}
	overrides := map[string]string{
		"LEARNING_RATE": "0.002",
		"BATCH_SIZE":    "64",
	}

	restore, err := ApplyOverrides(dir, constants, overrides)
	if err != nil {
		t.Fatal(err)
	}

	// Check overridden content.
	data, _ := os.ReadFile(filepath.Join(dir, "train.py"))
	content := string(data)
	if got := "lr = 0.002"; !strings.Contains(content, got) {
		t.Errorf("expected %q in content, got:\n%s", got, content)
	}
	if got := "batch_size = 64"; !strings.Contains(content, got) {
		t.Errorf("expected %q in content, got:\n%s", got, content)
	}
	// Verify comment is preserved.
	if got := "# learning rate"; !strings.Contains(content, got) {
		t.Errorf("expected comment preserved, got:\n%s", content)
	}

	// Restore.
	restore()

	data, _ = os.ReadFile(filepath.Join(dir, "train.py"))
	if string(data) != original {
		t.Errorf("restore failed: got %q, want %q", string(data), original)
	}
}

func TestApplyOverridesRestoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	original := "x = 1\n"
	if err := os.WriteFile(filepath.Join(dir, "f.py"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	constants := []ConstantDef{
		{Name: "X", File: "f.py", Pattern: `x\s*=\s*(\d+)`, Type: "int"},
	}
	restore, err := ApplyOverrides(dir, constants, map[string]string{"X": "2"})
	if err != nil {
		t.Fatal(err)
	}

	// Call restore twice — should not panic.
	restore()
	restore()

	data, _ := os.ReadFile(filepath.Join(dir, "f.py"))
	if string(data) != original {
		t.Errorf("restore failed: got %q, want %q", string(data), original)
	}
}

func TestApplyOverridesBoundsFloat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.py"), []byte("lr = 0.001\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	minVal := 0.0001
	maxVal := 0.1
	constants := []ConstantDef{
		{Name: "LR", File: "f.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float", Min: &minVal, Max: &maxVal},
	}

	// Value within bounds — should succeed.
	restore, err := ApplyOverrides(dir, constants, map[string]string{"LR": "0.05"})
	if err != nil {
		t.Fatal(err)
	}
	restore()

	// Value below min — should fail.
	_, err = ApplyOverrides(dir, constants, map[string]string{"LR": "0.00001"})
	if err == nil {
		t.Fatal("expected error for value below min")
	}

	// Value above max — should fail.
	_, err = ApplyOverrides(dir, constants, map[string]string{"LR": "0.5"})
	if err == nil {
		t.Fatal("expected error for value above max")
	}
}

func TestApplyOverridesBoundsInt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.py"), []byte("bs = 32\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	minVal := 8.0
	maxVal := 256.0
	constants := []ConstantDef{
		{Name: "BS", File: "f.py", Pattern: `bs\s*=\s*(\d+)`, Type: "int", Min: &minVal, Max: &maxVal},
	}

	_, err := ApplyOverrides(dir, constants, map[string]string{"BS": "4"})
	if err == nil {
		t.Fatal("expected error for value below min")
	}

	_, err = ApplyOverrides(dir, constants, map[string]string{"BS": "512"})
	if err == nil {
		t.Fatal("expected error for value above max")
	}
}

func TestParseConstantsLLMResponse(t *testing.T) {
	constants := []ConstantDef{
		{Name: "LEARNING_RATE", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
		{Name: "BATCH_SIZE", File: "train.py", Pattern: `batch_size\s*=\s*(\d+)`, Type: "int"},
	}

	resp := "HYPOTHESIS: double learning rate because loss converges too slowly\n\nLEARNING_RATE = 0.002\nBATCH_SIZE = 64\n"
	hyp, overrides := parseConstantsLLMResponse(resp, constants)

	if hyp != "double learning rate because loss converges too slowly" {
		t.Errorf("hypothesis = %q", hyp)
	}
	if overrides["LEARNING_RATE"] != "0.002" {
		t.Errorf("LEARNING_RATE = %q", overrides["LEARNING_RATE"])
	}
	if overrides["BATCH_SIZE"] != "64" {
		t.Errorf("BATCH_SIZE = %q", overrides["BATCH_SIZE"])
	}
}

func TestParseConstantsLLMResponseInvalidName(t *testing.T) {
	constants := []ConstantDef{
		{Name: "LR", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
	}

	resp := "HYPOTHESIS: test\n\nLR = 0.01\nUNKNOWN = 999\n"
	_, overrides := parseConstantsLLMResponse(resp, constants)

	if _, ok := overrides["UNKNOWN"]; ok {
		t.Error("should not accept unknown constant name")
	}
	if overrides["LR"] != "0.01" {
		t.Errorf("LR = %q, want %q", overrides["LR"], "0.01")
	}
}

func TestSaveLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(workdir, configDir), 0o755); err != nil {
		t.Fatal(err)
	}

	ov := &OverrideSet{Values: map[string]string{
		"LEARNING_RATE": "0.002",
		"BATCH_SIZE":    "64",
	}}
	if err := SaveOverrides(workdir, ov); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadOverrides(workdir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Values["LEARNING_RATE"] != "0.002" {
		t.Errorf("LEARNING_RATE = %q", loaded.Values["LEARNING_RATE"])
	}
	if loaded.Values["BATCH_SIZE"] != "64" {
		t.Errorf("BATCH_SIZE = %q", loaded.Values["BATCH_SIZE"])
	}
}

func TestConfigIsConstantsMode(t *testing.T) {
	cfg := &Config{}
	if cfg.IsConstantsMode() {
		t.Error("empty constants should not be constants mode")
	}

	cfg.Constants = []ConstantDef{{Name: "X"}}
	if !cfg.IsConstantsMode() {
		t.Error("non-empty constants should be constants mode")
	}
}

func TestConfigValidateConstants(t *testing.T) {
	base := Config{
		TargetFiles:     []string{"train.py"},
		MetricCmd:       "python train.py",
		MetricName:      "val_loss",
		MetricDirection: "minimize",
		BranchTag:       "test",
	}

	// Valid constants.
	cfg := base
	cfg.Constants = []ConstantDef{
		{Name: "LR", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid constants should pass: %v", err)
	}

	// Missing name.
	cfg = base
	cfg.Constants = []ConstantDef{
		{File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("missing name should fail")
	}

	// File not in target_files.
	cfg = base
	cfg.Constants = []ConstantDef{
		{Name: "LR", File: "other.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "float"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("file not in target_files should fail")
	}

	// Invalid pattern.
	cfg = base
	cfg.Constants = []ConstantDef{
		{Name: "LR", File: "train.py", Pattern: `[invalid`, Type: "float"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("invalid pattern should fail")
	}

	// Invalid type.
	cfg = base
	cfg.Constants = []ConstantDef{
		{Name: "LR", File: "train.py", Pattern: `lr\s*=\s*([\d.]+)`, Type: "boolean"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("invalid type should fail")
	}
}

func TestReplaceCapture(t *testing.T) {
	content := "lr = 0.001  # learning rate\nbatch_size = 32\n"

	// Replace lr value, preserving comment.
	result, err := replaceCapture(content, `lr\s*=\s*([\d.]+)`, "0.002")
	if err != nil {
		t.Fatal(err)
	}
	want := "lr = 0.002  # learning rate\nbatch_size = 32\n"
	if result != want {
		t.Errorf("got:\n%s\nwant:\n%s", result, want)
	}
}
