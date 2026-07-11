package jsonutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadFile decodes a JSON file into destination. Missing files are reported as
// exists=false without error. Label prefixes stable domain-facing errors.
func LoadFile(path string, destination any, label string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("%s: read %s: %w", label, path, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return true, fmt.Errorf("%s: parse %s: %w", label, path, err)
	}
	return true, nil
}

// WriteFile writes indented JSON through a same-directory temporary file and
// atomic rename, creating parent directories when needed.
func WriteFile(path string, value any, label string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: mkdir: %w", label, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil { //nolint:gosec // G306 — single-user host data
		return fmt.Errorf("%s: write %s: %w", label, tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("%s: rename: %w", label, err)
	}
	return nil
}
