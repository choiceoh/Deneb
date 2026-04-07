// install.go handles skill dependency installation via brew, npm, go, uv, or download.
//
// This ports src/agents/skills-install.ts to Go.
package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// InstallRequest describes a skill installation request.
type InstallRequest struct {
	WorkspaceDir string
	SkillName    string
	InstallID    string
	TimeoutMs    int64
	Spec         SkillInstallSpec
	Logger       *slog.Logger
}

// InstallResult holds the outcome of a skill installation.
type InstallResult struct {
	OK       bool     `json:"ok"`
	Message  string   `json:"message"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Code     *int     `json:"code,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// InstallSkillDep installs a single skill dependency spec.
func InstallSkillDep(req InstallRequest) *InstallResult {
	log := req.Logger
	if log == nil {
		log = slog.Default()
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	spec := req.Spec
	switch spec.Kind {
	case "brew":
		return runInstallCommand(ctx, log, "brew", []string{"install", spec.Formula})
	case "node":
		return runInstallCommand(ctx, log, "npm", []string{"install", "-g", spec.Package})
	case "go":
		return runInstallCommand(ctx, log, "go", []string{"install", spec.Module})
	case "uv":
		return runInstallCommand(ctx, log, "uv", []string{"tool", "install", spec.Package})
	case "download":
		return installDownload(ctx, log, spec)
	default:
		return &InstallResult{
			OK:      false,
			Message: fmt.Sprintf("unsupported install kind: %q", spec.Kind),
		}
	}
}

func runInstallCommand(ctx context.Context, log *slog.Logger, name string, args []string) *InstallResult {
	log.Info("running install command", "cmd", name, "args", args)

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	if err != nil {
		return &InstallResult{
			OK:      false,
			Message: fmt.Sprintf("install failed: %v", err),
			Stdout:  string(out),
			Code:    &exitCode,
		}
	}

	return &InstallResult{
		OK:      true,
		Message: fmt.Sprintf("installed via %s", name),
		Stdout:  string(out),
		Code:    &exitCode,
	}
}

func installDownload(ctx context.Context, log *slog.Logger, spec SkillInstallSpec) *InstallResult {
	if spec.URL == "" {
		return &InstallResult{OK: false, Message: "download spec missing URL"}
	}

	log.Info("downloading skill dependency", "url", spec.URL)

	// Use curl for downloading.
	args := []string{"-fsSL", "-o", "/tmp/deneb-skill-download", spec.URL}
	cmd := exec.CommandContext(ctx, "curl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &InstallResult{
			OK:      false,
			Message: fmt.Sprintf("download failed: %v", err),
			Stdout:  string(out),
		}
	}

	// Handle extraction if needed.
	if spec.Extract != nil && *spec.Extract {
		targetDir := spec.TargetDir
		if targetDir == "" {
			targetDir = "/usr/local/bin"
		}
		extractArgs := []string{"-xf", "/tmp/deneb-skill-download", "-C", targetDir}
		if spec.StripComponents != nil && *spec.StripComponents > 0 {
			extractArgs = append(extractArgs, fmt.Sprintf("--strip-components=%d", *spec.StripComponents))
		}
		archive := strings.ToLower(spec.Archive)
		if strings.Contains(archive, "zip") || strings.HasSuffix(spec.URL, ".zip") {
			// Use unzip for zip files.
			unzipArgs := []string{"-o", "/tmp/deneb-skill-download", "-d", targetDir}
			cmd = exec.CommandContext(ctx, "unzip", unzipArgs...)
		} else {
			cmd = exec.CommandContext(ctx, "tar", extractArgs...)
		}
		extractOut, err := cmd.CombinedOutput()
		if err != nil {
			return &InstallResult{
				OK:      false,
				Message: fmt.Sprintf("extraction failed: %v", err),
				Stdout:  string(extractOut),
			}
		}
	}

	return &InstallResult{
		OK:      true,
		Message: "downloaded and installed",
	}
}
