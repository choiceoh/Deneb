package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// prepareConfigMap creates the private config directory and loads the current
// JSON object. A missing file is represented by an empty object so callers can
// use the same mutation path for first-run and existing installations.
func prepareConfigMap(configPath string) (map[string]any, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating config directory: %w", err)
	}
	raw, _, err := readConfigMap(configPath)
	return raw, err
}

// readConfigMap loads the raw JSON object without creating directories.
// exists distinguishes an absent file from an existing empty object for
// idempotent delete operations.
func readConfigMap(configPath string) (raw map[string]any, exists bool, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), false, nil
		}
		return nil, false, fmt.Errorf("reading config: %w", err)
	}
	raw = make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, true, fmt.Errorf("parsing config: %w", err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	return raw, true, nil
}

// writeConfigMap records the common mutation timestamp and persists the raw
// object with the config file's established formatting and permissions.
func writeConfigMap(configPath string, raw map[string]any) error {
	ensureObject(raw, "meta")["lastTouchedAt"] = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func ensureObject(parent map[string]any, key string) map[string]any {
	if obj, ok := parent[key].(map[string]any); ok {
		return obj
	}
	obj := make(map[string]any)
	parent[key] = obj
	return obj
}
