package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolGateway returns a tool that exposes gateway management actions (config inspection,
// patching, and restart) rooted at repoDir.
func ToolGateway(repoDir string) ToolFunc {
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
			// Do NOT restart immediately. Ask the user for confirmation first.
			// The agent must relay this message to the user and wait for explicit
			// approval before calling "restart.confirmed".
			return "⚠️ 게이트웨이 재시작이 필요합니다. 재시작하면 진행 중인 세션이 중단됩니다.\n\n사용자에게 재시작 확인을 요청하세요. 사용자가 승인하면 action: \"restart.confirmed\"로 다시 호출하세요.", nil

		case "restart.confirmed":
			// User has explicitly confirmed the restart.
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
			if err := os.WriteFile(sentinelPath, sentinelData, 0644); err != nil {
				slog.Warn("gateway: failed to write update sentinel", "path", sentinelPath, "err", err)
			}

			return fmt.Sprintf("Update completed successfully.\nGit pull: %s\nBuild: OK\nRestart the gateway to apply changes.", strings.TrimSpace(string(pullOut))), nil

		default:
			return fmt.Sprintf("Unknown gateway action: %q. Supported: config.get, config.schema.lookup, config.patch, config.apply, restart, restart.confirmed, update.run", p.Action), nil
		}
	}
}
