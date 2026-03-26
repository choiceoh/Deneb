package autoreply

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// StagedMediaMaxBytes is the maximum file size for staged media.
const StagedMediaMaxBytes int64 = 50 * 1024 * 1024 // 50 MB (matches Telegram limit)

// StageSandboxMediaParams holds the parameters for media staging.
type StageSandboxMediaParams struct {
	Ctx          *MsgContext
	SessionKey   string
	WorkspaceDir string
	MediaDir     string // base media directory for local files
	Logger       *slog.Logger
}

// StageSandboxMedia stages inbound media files into a sandbox workspace
// for agent access. Supports both local and remote (SCP) media sources.
//
// Mirrors src/auto-reply/reply/stage-sandbox-media.ts stageSandboxMedia().
func StageSandboxMedia(params StageSandboxMediaParams) error {
	ctx := params.Ctx
	if ctx == nil {
		return nil
	}

	rawPaths := resolveRawMediaPaths(ctx)
	if len(rawPaths) == 0 || params.SessionKey == "" {
		return nil
	}

	logger := params.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Determine staging directory.
	destDir := filepath.Join(params.WorkspaceDir, "media", "inbound")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}

	usedNames := make(map[string]bool)
	staged := make(map[string]string) // absolute source → staged path

	for _, raw := range rawPaths {
		source := resolveAbsolutePath(raw)
		if source == "" || staged[source] != "" {
			continue
		}

		// Validate source path is within allowed roots.
		if params.MediaDir != "" && !isAllowedLocalPath(source, params.MediaDir) {
			logger.Debug("blocking media staging from outside media directory",
				"source", source,
				"mediaDir", params.MediaDir,
			)
			continue
		}

		fileName := allocateStagedFileName(source, usedNames)
		if fileName == "" {
			continue
		}

		destPath := filepath.Join(destDir, fileName)
		if err := stageLocalFile(source, destPath, StagedMediaMaxBytes); err != nil {
			if errors.Is(err, errFileTooLarge) {
				logger.Debug("blocking inbound media staging above size limit",
					"source", source,
					"maxBytes", StagedMediaMaxBytes,
				)
			} else {
				logger.Debug("failed to stage inbound media",
					"source", source,
					"error", err,
				)
			}
			continue
		}

		relativePath := filepath.Join("media", "inbound", fileName)
		staged[source] = relativePath
	}

	// Rewrite media paths in context.
	if len(staged) > 0 {
		rewriteStagedMediaPaths(ctx, rawPaths, staged)
	}
	return nil
}

// StageRemoteMedia stages a file from a remote host via SCP.
func StageRemoteMedia(remoteHost, remotePath, localPath string) error {
	safeHost := strings.TrimSpace(remoteHost)
	safePath := strings.TrimSpace(remotePath)
	if safeHost == "" || safePath == "" {
		return errors.New("invalid remote host or path for SCP")
	}

	cmd := exec.Command("/usr/bin/scp",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"--",
		fmt.Sprintf("%s:%s", safeHost, safePath),
		localPath,
	)
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("scp failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// --- internal helpers ---

var errFileTooLarge = errors.New("file too large")

func resolveRawMediaPaths(ctx *MsgContext) []string {
	if ctx.MediaPath != "" {
		path := strings.TrimSpace(ctx.MediaPath)
		if path != "" {
			return []string{path}
		}
	}
	return nil
}

func resolveAbsolutePath(value string) string {
	resolved := strings.TrimSpace(value)
	if resolved == "" {
		return ""
	}
	// Handle file:// URLs.
	if strings.HasPrefix(resolved, "file://") {
		resolved = strings.TrimPrefix(resolved, "file://")
	}
	if !filepath.IsAbs(resolved) {
		return ""
	}
	return resolved
}

func isAllowedLocalPath(filePath, mediaDir string) bool {
	// Clean paths for comparison.
	cleanFile := filepath.Clean(filePath)
	cleanMedia := filepath.Clean(mediaDir)
	// File must be within the media directory.
	return strings.HasPrefix(cleanFile, cleanMedia+string(filepath.Separator)) ||
		cleanFile == cleanMedia
}

func allocateStagedFileName(source string, usedNames map[string]bool) string {
	baseName := filepath.Base(source)
	if baseName == "" || baseName == "." {
		return ""
	}

	ext := filepath.Ext(baseName)
	nameOnly := strings.TrimSuffix(baseName, ext)

	fileName := baseName
	suffix := 1
	for usedNames[fileName] {
		fileName = fmt.Sprintf("%s-%d%s", nameOnly, suffix, ext)
		suffix++
	}
	usedNames[fileName] = true
	return fileName
}

func stageLocalFile(source, dest string, maxBytes int64) error {
	// Check file size.
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return errFileTooLarge
	}

	// Ensure destination directory exists.
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	// Copy file.
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("write dest: %w", err)
	}
	return nil
}

func rewriteStagedMediaPaths(ctx *MsgContext, rawPaths []string, staged map[string]string) {
	// Rewrite single MediaPath.
	if ctx.MediaPath != "" {
		abs := resolveAbsolutePath(ctx.MediaPath)
		if abs != "" {
			if mapped, ok := staged[abs]; ok {
				ctx.MediaPath = mapped
			}
		}
	}
}
