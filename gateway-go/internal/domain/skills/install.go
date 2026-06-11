// install.go handles skill dependency installation via brew, npm, go, uv, or download.
//
// This ports src/agents/skills-install.ts to Go.
package skills

import (
	"log/slog"
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
