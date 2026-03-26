package vega

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ModelInfo describes a detected GGUF model file.
type ModelInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AutoDetectModels scans a directory for .gguf model files.
// Returns an empty slice if the directory doesn't exist or contains no models.
func AutoDetectModels(dir string) []ModelInfo {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var models []ModelInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		models = append(models, ModelInfo{
			Path: filepath.Join(dir, e.Name()),
			Name: e.Name(),
			Size: info.Size(),
		})
	}
	return models
}

// DefaultModelDir returns the default directory for GGUF models (~/.deneb/models/).
func DefaultModelDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "models")
}

// ShouldEnableVega determines whether the Vega backend should be activated.
// Requires: FFI available + at least one GGUF model detected (or explicit config).
func ShouldEnableVega(ffiAvailable bool, modelDir string, logger *slog.Logger) bool {
	if !ffiAvailable {
		if logger != nil {
			logger.Debug("vega: FFI not available, skipping activation")
		}
		return false
	}

	if modelDir == "" {
		modelDir = DefaultModelDir()
	}

	models := AutoDetectModels(modelDir)
	if len(models) > 0 {
		if logger != nil {
			logger.Info("vega: detected GGUF models", "count", len(models), "dir", modelDir)
			for _, m := range models {
				logger.Debug("vega: model", "name", m.Name, "size", m.Size)
			}
		}
		return true
	}

	// No models found, but Vega FTS (non-ML) can still work with FFI.
	// Enable Vega even without models for FTS-only mode.
	if logger != nil {
		logger.Info("vega: no GGUF models found, enabling FTS-only mode", "dir", modelDir)
	}
	return true
}
