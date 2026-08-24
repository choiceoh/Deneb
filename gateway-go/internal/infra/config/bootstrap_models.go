// bootstrap_models.go — default/role model persistence for the gateway config:
// writes agents.defaultModel and per-role agents.*Model fields into the config
// file while preserving all other fields.
// Split from bootstrap.go (pure move, no behavior change).
package config

import (
	"fmt"
	"log/slog"
)

// PersistDefaultModel writes the given model ID into agents.defaultModel
// in the config file, preserving all other fields.
func PersistDefaultModel(configPath, model string, logger *slog.Logger) error {
	raw, err := prepareConfigMap(configPath)
	if err != nil {
		return err
	}

	// Set agents.defaultModel.
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents["defaultModel"] = model

	if err := writeConfigMap(configPath, raw); err != nil {
		return err
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
//	coding      → agents.codingModel
//	fallback    → agents.fallbackModel
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
	case "coding":
		field = "codingModel"
	case "fallback":
		field = "fallbackModel"
	case "vision":
		field = "visionModel"
	case "submain":
		field = "submainModel"
	default:
		return fmt.Errorf("unknown model role %q", role)
	}

	raw, err := prepareConfigMap(configPath)
	if err != nil {
		return err
	}

	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		agents = make(map[string]any)
		raw["agents"] = agents
	}
	agents[field] = model

	if err := writeConfigMap(configPath, raw); err != nil {
		return err
	}

	logger.Info("persisted role model", "role", role, "field", field, "model", model, "path", configPath)
	return nil
}
