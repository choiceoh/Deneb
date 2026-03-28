package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

func gatewayToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Gateway action",
				"enum":        []string{"restart", "config.get", "config.schema.lookup", "config.apply", "config.patch", "update.run"},
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Config path for schema.lookup",
			},
			"raw": map[string]any{
				"type":        "string",
				"description": "Raw config JSON for apply/patch",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Reason for restart",
			},
		},
		"required": []string{"action"},
	}
}

func toolGateway(repoDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string         `json:"action"`
			Path   string         `json:"path"`
			Patch  map[string]any `json:"patch"`
			Config map[string]any `json:"config"`
		}
		if err := jsonutil.UnmarshalInto("gateway params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "config.get":
			snapshot, err := config.LoadConfigFromDefaultPath()
			if err != nil {
				return "Failed to load config: " + err.Error(), nil
			}
			result := map[string]any{
				"path":   snapshot.Path,
				"exists": snapshot.Exists,
				"valid":  snapshot.Valid,
				"hash":   snapshot.Hash,
				"config": snapshot.Config,
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil

		case "config.schema.lookup":
			node := config.LookupSchema(p.Path)
			if node == nil {
				return fmt.Sprintf("No schema found for path %q.", p.Path), nil
			}
			data, _ := json.MarshalIndent(node, "", "  ")
			return string(data), nil

		case "config.patch":
			if p.Patch == nil {
				return "", fmt.Errorf("patch object is required for config.patch")
			}
			snapshot, err := config.LoadConfigFromDefaultPath()
			if err != nil {
				return "Failed to load config: " + err.Error(), nil
			}
			// Parse current config as map and merge patch.
			var current map[string]any
			if err := json.Unmarshal([]byte(snapshot.Raw), &current); err != nil {
				return "Failed to parse current config: " + err.Error(), nil
			}
			for k, v := range p.Patch {
				current[k] = v
			}
			merged, err := json.MarshalIndent(current, "", "  ")
			if err != nil {
				return "Failed to serialize patched config: " + err.Error(), nil
			}
			cfgPath := config.ResolveConfigPath()
			if err := os.WriteFile(cfgPath, merged, 0644); err != nil {
				return "Failed to write config: " + err.Error(), nil
			}
			return fmt.Sprintf("Config patched successfully. Written to %s", cfgPath), nil

		case "config.apply":
			if p.Config == nil {
				return "", fmt.Errorf("config object is required for config.apply")
			}
			data, err := json.MarshalIndent(p.Config, "", "  ")
			if err != nil {
				return "Failed to serialize config: " + err.Error(), nil
			}
			cfgPath := config.ResolveConfigPath()
			if err := os.WriteFile(cfgPath, data, 0644); err != nil {
				return "Failed to write config: " + err.Error(), nil
			}
			return fmt.Sprintf("Config applied successfully. Written to %s", cfgPath), nil

		case "restart":
			// Send SIGUSR1 to trigger graceful restart.
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return "Failed to find gateway process: " + err.Error(), nil
			}
			if err := proc.Signal(syscall.SIGUSR1); err != nil {
				return "Gateway restart via SIGUSR1 failed: " + err.Error() + ". Use `deneb gateway restart` from the CLI.", nil
			}
			return "Gateway restart signal sent (SIGUSR1). The gateway will restart shortly.", nil

		case "update.run":
			dir := repoDir
			if dir == "" {
				dir, _ = os.Getwd()
			}
			updateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			// Step 1: git pull
			pullCmd := exec.CommandContext(updateCtx, "git", "pull", "--rebase", "origin", "main")
			pullCmd.Dir = dir
			pullOut, pullErr := pullCmd.CombinedOutput()
			if pullErr != nil {
				return fmt.Sprintf("Update failed at git pull:\n%s\n%s", string(pullOut), pullErr.Error()), nil
			}

			// Step 2: make all
			buildCmd := exec.CommandContext(updateCtx, "make", "all")
			buildCmd.Dir = dir
			buildOut, buildErr := buildCmd.CombinedOutput()
			if buildErr != nil {
				return fmt.Sprintf("Update failed at build:\n%s\n%s", string(buildOut), buildErr.Error()), nil
			}

			// Write sentinel file.
			home, _ := os.UserHomeDir()
			sentinelPath := home + "/.deneb/.update-sentinel"
			sentinel := map[string]any{
				"updatedAt": time.Now().Format(time.RFC3339),
			}
			sentinelData, _ := json.Marshal(sentinel)
			_ = os.WriteFile(sentinelPath, sentinelData, 0644)

			return fmt.Sprintf("Update completed successfully.\nGit pull: %s\nBuild: OK\nRestart the gateway to apply changes.", strings.TrimSpace(string(pullOut))), nil

		default:
			return fmt.Sprintf("Unknown gateway action: %q. Supported: config.get, config.schema.lookup, config.patch, config.apply, restart, update.run", p.Action), nil
		}
	}
}

// --- sessions_list tool ---
