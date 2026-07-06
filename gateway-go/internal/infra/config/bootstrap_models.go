// bootstrap_models.go — default/role model persistence for the gateway config:
// writes agents.defaultModel and per-role agents.*Model fields into the config
// file while preserving all other fields.
// Split from bootstrap.go (pure move, no behavior change).
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// PersistDefaultModel writes the given model ID into agents.defaultModel
// in the config file, preserving all other fields.
func PersistDefaultModel(configPath, model string, logger *slog.Logger) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	// Set agents.defaultModel.
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents["defaultModel"] = model

	// Update meta.
	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		raw["meta"] = meta
	}
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	logger.Info("persisted default model", "model", model, "path", configPath)
	return nil
}

// PersistRoleModel writes a model ID into the agents config field for the
// given modelrole role, preserving all other fields:
//
//	main        → agents.defaultModel
//	tiny        → agents.tinyModel
//	lightweight → agents.lightweightModel
//	analysis    → agents.analysisModel
//	coding      → agents.codingModel
//	fallback    → agents.fallbackModel
//	chatbot     → agents.chatbotModel
//	vision      → agents.visionModel
//
// Mirrors PersistDefaultModel; used by the miniapp per-role model picker.
func PersistRoleModel(configPath, role, model string, logger *slog.Logger) error {
	var field string
	switch role {
	case "main", "":
		field = "defaultModel"
	case "tiny":
		field = "tinyModel"
	case "lightweight":
		field = "lightweightModel"
	case "analysis":
		field = "analysisModel"
	case "coding":
		field = "codingModel"
	case "fallback":
		field = "fallbackModel"
	case "chatbot":
		field = "chatbotModel"
	case "vision":
		field = "visionModel"
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	var raw map[string]any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading config: %w", err)
		}
		raw = make(map[string]any)
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents[field] = model

	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		raw["meta"] = meta
	}
	meta["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	logger.Info("persisted role model", "role", role, "field", field, "model", model, "path", configPath)
	return nil
}
