// Discord attachment download, workspace utilities, and dashboard building.
package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
)

// maxAttachmentSize is the max file size to download from Discord (1 MB).
const maxAttachmentSize = 1 * 1024 * 1024

// downloadDiscordAttachments downloads file attachments from a Discord message
// and converts them to ChatAttachments for the agent pipeline.
func (p *InboundProcessor) downloadDiscordAttachments(attachments []discord.Attachment) []chat.ChatAttachment {
	var result []chat.ChatAttachment
	for _, att := range attachments {
		if att.Size > maxAttachmentSize {
			p.logger.Info("skipping large discord attachment",
				"filename", att.Filename, "size", att.Size)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		data, err := downloadURL(ctx, att.URL)
		cancel()
		if err != nil {
			p.logger.Warn("failed to download discord attachment",
				"filename", att.Filename, "error", err)
			continue
		}

		// Determine type: code files → "file", images → "image".
		attType := "file"
		lang := discord.DetectCodeLanguage(att.Filename)
		if isImageFilename(att.Filename) {
			attType = "image"
		}

		ca := chat.ChatAttachment{
			Type:     attType,
			Name:     att.Filename,
			Data:     base64.StdEncoding.EncodeToString(data),
			MimeType: guessMimeType(att.Filename),
		}
		_ = lang // language info available if needed for context

		result = append(result, ca)
	}
	return result
}

// downloadURL fetches raw bytes from a URL.
func downloadURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAttachmentSize+1))
}

// isImageFilename checks if a filename looks like an image.
func isImageFilename(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// guessMimeType returns a MIME type based on file extension.
func guessMimeType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".go"):
		return "text/x-go"
	case strings.HasSuffix(lower, ".py"):
		return "text/x-python"
	case strings.HasSuffix(lower, ".js"):
		return "text/javascript"
	case strings.HasSuffix(lower, ".ts"):
		return "text/typescript"
	case strings.HasSuffix(lower, ".rs"):
		return "text/x-rust"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "text/yaml"
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// buildWorkspaceContext gathers lightweight workspace info for first-message context.
// Returns a formatted string with git branch, short status, and project root files.
func buildWorkspaceContext(workspaceDir string) string {
	if _, err := os.Stat(workspaceDir); err != nil {
		return ""
	}

	var parts []string

	// Git branch + short status.
	if branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		parts = append(parts, "**Branch:** `"+branch+"`")
	}
	if status := runGitCmd(workspaceDir, "status", "--short"); status != "" {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		if len(lines) > 15 {
			lines = append(lines[:15], fmt.Sprintf("... and %d more files", len(lines)-15))
		}
		parts = append(parts, "**Git Status:**\n```\n"+strings.Join(lines, "\n")+"\n```")
	} else if len(parts) > 0 {
		parts = append(parts, "**Git Status:** clean")
	}

	// Top-level directory listing.
	if ls := runCmd(workspaceDir, "ls", "-1"); ls != "" {
		lines := strings.Split(strings.TrimSpace(ls), "\n")
		if len(lines) > 20 {
			lines = append(lines[:20], fmt.Sprintf("... and %d more", len(lines)-20))
		}
		parts = append(parts, "**Project Root:**\n```\n"+strings.Join(lines, "\n")+"\n```")
	}

	if len(parts) == 0 {
		return ""
	}

	return "## Workspace Context\n`" + workspaceDir + "`\n\n" + strings.Join(parts, "\n")
}

// runGitCmd runs a git command in the given directory and returns trimmed stdout.
func runGitCmd(dir string, args ...string) string {
	return runCmd(dir, "git", args...)
}

// runCmd runs a command in the given directory with a 5-second timeout.
func runCmd(dir string, name string, args ...string) string {
	return runCmdWithTimeout(dir, 5*time.Second, name, args...)
}

// runCmdWithTimeout runs a command with a custom timeout. Returns combined
// stdout+stderr trimmed output.
func runCmdWithTimeout(dir string, timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// Return partial output even on error (useful for build/test failures).
		if s := strings.TrimSpace(out.String()); s != "" {
			return s
		}
		return ""
	}
	return strings.TrimSpace(out.String())
}

// detectDefaultBranch returns the default branch name (main or master)
// by checking the remote HEAD. Falls back to "main".
func detectDefaultBranch(dir string) string {
	ref := runGitCmd(dir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if ref != "" {
		// e.g. "refs/remotes/origin/main" → "origin/main"
		parts := strings.SplitN(ref, "refs/remotes/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	// Fallback: check if origin/main exists.
	if out := runGitCmd(dir, "rev-parse", "--verify", "origin/main"); out != "" {
		return "origin/main"
	}
	if out := runGitCmd(dir, "rev-parse", "--verify", "origin/master"); out != "" {
		return "origin/master"
	}
	return "origin/main"
}

// detectProjectType determines the project type from marker files.
func detectProjectType(dir string) string {
	markers := map[string]string{
		"go.mod":           "go",
		"Cargo.toml":       "rust",
		"package.json":     "node",
		"pyproject.toml":   "python",
		"setup.py":         "python",
		"requirements.txt": "python",
		"Makefile":         "make",
	}
	for file, lang := range markers {
		if _, err := os.Stat(dir + "/" + file); err == nil {
			return lang
		}
	}
	return ""
}

// testCommand returns the test command for a project type.
func testCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"test", "./..."}
	case "rust":
		return "cargo", []string{"test"}
	case "node":
		return "npm", []string{"test"}
	case "python":
		return "python", []string{"-m", "pytest"}
	case "make":
		return "make", []string{"test"}
	}
	return "", nil
}

// buildCommand returns the build command for a project type.
func buildCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"build", "./..."}
	case "rust":
		return "cargo", []string{"build"}
	case "node":
		return "npm", []string{"run", "build"}
	case "make":
		return "make", []string{"all"}
	}
	return "", nil
}

// lintCommand returns the lint/vet command for a project type.
func lintCommand(projType string) (string, []string) {
	switch projType {
	case "go":
		return "go", []string{"vet", "./..."}
	case "rust":
		return "cargo", []string{"clippy", "--workspace", "--", "-D", "warnings"}
	case "node":
		return "npx", []string{"eslint", "."}
	case "python":
		return "python", []string{"-m", "ruff", "check", "."}
	}
	return "", nil
}

// buildEnhancedDashboard creates the enhanced dashboard with lint, stash, upstream info.
func (p *InboundProcessor) buildEnhancedDashboard(workspaceDir, sessionKey string) ([]discord.Embed, []discord.Component) {
	branch := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "HEAD")
	status := runGitCmd(workspaceDir, "status", "--short")
	recentLog := runGitCmd(workspaceDir, "log", "--oneline", "-5", "--no-color")

	// Count changed files and build summary.
	changedFiles := 0
	filesSummary := ""
	if status != "" {
		lines := strings.Split(strings.TrimSpace(status), "\n")
		changedFiles = len(lines)
		// Show first 5 files.
		var summaryLines []string
		for i, line := range lines {
			if i >= 5 {
				summaryLines = append(summaryLines, fmt.Sprintf("... 외 %d개", len(lines)-5))
				break
			}
			summaryLines = append(summaryLines, "`"+strings.TrimSpace(line)+"`")
		}
		filesSummary = strings.Join(summaryLines, "\n")
	}

	// Upstream tracking info.
	upstream := runGitCmd(workspaceDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")

	// Stash count.
	stashCount := 0
	if stashOut := runGitCmd(workspaceDir, "stash", "list"); stashOut != "" {
		stashCount = len(strings.Split(strings.TrimSpace(stashOut), "\n"))
	}

	// Build/test/lint status (concurrent).
	projType := detectProjectType(workspaceDir)
	buildStatus := "⏭️ 미확인"
	testStatus := "⏭️ 미확인"
	lintStatus := "⏭️ 미확인"

	if cmdName, cmdArgs := buildCommand(projType); cmdName != "" {
		buildOut := runCmdWithTimeout(workspaceDir, 15*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(buildOut)
		if buildOut == "" || (!strings.Contains(lower, "error") && !strings.Contains(lower, "fail")) {
			buildStatus = "✅ 성공"
		} else {
			buildStatus = "❌ 실패"
		}
	}

	if cmdName, cmdArgs := testCommand(projType); cmdName != "" {
		testOut := runCmdWithTimeout(workspaceDir, 30*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(testOut)
		if testOut == "" || (!strings.Contains(lower, "fail") && !strings.Contains(lower, "error") && !strings.Contains(lower, "panic")) {
			testStatus = "✅ 전체 통과"
		} else {
			testStatus = "❌ 일부 실패"
		}
	}

	if cmdName, cmdArgs := lintCommand(projType); cmdName != "" {
		lintOut := runCmdWithTimeout(workspaceDir, 15*time.Second, cmdName, cmdArgs...)
		lower := strings.ToLower(lintOut)
		if lintOut == "" || (!strings.Contains(lower, "error") && !strings.Contains(lower, "warning")) {
			lintStatus = "✅ 깨끗"
		} else if strings.Contains(lower, "error") {
			lintStatus = "❌ 오류 있음"
		} else {
			lintStatus = "⚠️ 경고 있음"
		}
	}

	// Format recent commits.
	commitSummary := "커밋 없음"
	if recentLog != "" {
		lines := strings.Split(strings.TrimSpace(recentLog), "\n")
		var commitLines []string
		for _, line := range lines {
			if len(line) > 0 {
				commitLines = append(commitLines, "• "+line)
			}
		}
		commitSummary = strings.Join(commitLines, "\n")
	}

	data := discord.DashboardData{
		Branch:       branch,
		ChangedFiles: changedFiles,
		FilesSummary: filesSummary,
		BuildStatus:  buildStatus,
		TestStatus:   testStatus,
		LintStatus:   lintStatus,
		RecentLog:    commitSummary,
		Upstream:     upstream,
		StashCount:   stashCount,
	}

	embed := discord.FormatEnhancedDashboardEmbed(data)
	buttons := discord.DashboardButtons(sessionKey)
	return []discord.Embed{embed}, buttons
}

// changedGoPackages returns the Go packages that have uncommitted changes.
// Uses git diff to find changed .go files and maps them to packages.
func changedGoPackages(workspaceDir string) []string {
	diff := runGitCmd(workspaceDir, "diff", "--name-only", "HEAD")
	if diff == "" {
		// Also check untracked files.
		diff = runGitCmd(workspaceDir, "ls-files", "--others", "--exclude-standard")
	}
	if diff == "" {
		return nil
	}

	pkgSet := make(map[string]bool)
	for _, file := range strings.Split(strings.TrimSpace(diff), "\n") {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		dir := file
		if idx := strings.LastIndex(file, "/"); idx >= 0 {
			dir = file[:idx]
		} else {
			dir = "."
		}
		pkgSet["./"+dir+"/..."] = true
	}

	pkgs := make([]string, 0, len(pkgSet))
	for pkg := range pkgSet {
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}
