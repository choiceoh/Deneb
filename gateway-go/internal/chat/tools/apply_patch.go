package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolApplyPatch applies a unified diff patch using git apply.
// Supports dry_run mode for pre-validation.
func ToolApplyPatch(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Patch  string `json:"patch"`
			Strip  int    `json:"strip"`
			DryRun bool   `json:"dry_run"`
		}
		if err := jsonutil.UnmarshalInto("apply_patch params", input, &p); err != nil {
			return "", err
		}
		if p.Patch == "" {
			return "", fmt.Errorf("patch is required")
		}
		if patchContainsSymlinkMode(p.Patch) {
			return "", fmt.Errorf("patch apply failed: symlink patches are not allowed")
		}

		strip := p.Strip
		if strip < 0 {
			strip = 1
		}

		// Write patch to temp file.
		tmpFile, err := os.CreateTemp("", "deneb-patch-*.diff")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := tmpFile.WriteString(p.Patch); err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("failed to write patch: %w", err)
		}
		tmpFile.Close()

		// Build git apply command.
		args := []string{"apply", fmt.Sprintf("-p%d", strip)}
		if p.DryRun {
			args = append(args, "--check")
		}
		args = append(args, "--verbose", tmpPath)

		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = defaultDir
		out, err := cmd.CombinedOutput()
		output := strings.TrimSpace(string(out))

		if err != nil {
			if p.DryRun {
				return fmt.Sprintf("Patch validation FAILED:\n%s", output), nil
			}
			return "", fmt.Errorf("patch apply failed:\n%s", output)
		}

		if p.DryRun {
			if output == "" {
				return "Patch validation OK: patch applies cleanly.", nil
			}
			return fmt.Sprintf("Patch validation OK: patch applies cleanly.\n%s", output), nil
		}

		if output == "" {
			return "Patch applied successfully.", nil
		}
		return fmt.Sprintf("Patch applied successfully.\n%s", output), nil
	}
}

func patchContainsSymlinkMode(patch string) bool {
	for _, line := range strings.Split(patch, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "new file mode 120000") ||
			strings.HasPrefix(trimmed, "old mode 120000") {
			return true
		}

		// Existing symlink updates are represented as:
		// index <old-sha>..<new-sha> 120000
		if strings.HasPrefix(trimmed, "index ") && strings.HasSuffix(trimmed, " 120000") {
			return true
		}
	}
	return false
}
