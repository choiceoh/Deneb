package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolInspect provides deep code inspection by combining analyze, read, and
// git operations into a single tool call.
func ToolInspect(defaultDir string) ToolFunc {
	analyzeFn := ToolAnalyze(defaultDir)

	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			File   string `json:"file"`
			Symbol string `json:"symbol"`
			Depth  string `json:"depth"`
		}
		if err := jsonutil.UnmarshalInto("inspect params", input, &p); err != nil {
			return "", err
		}
		if p.File == "" {
			return "", fmt.Errorf("file is required")
		}

		depth := p.Depth
		if depth == "" {
			depth = "shallow"
		}

		// Auto-promote to symbol depth when symbol is specified.
		if p.Symbol != "" && depth == "shallow" {
			depth = "symbol"
		}

		filePath := ResolvePath(p.File, defaultDir)
		displayPath := p.File
		if rel, err := filepath.Rel(defaultDir, filePath); err == nil {
			displayPath = rel
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "# Inspect: %s", displayPath)
		if p.Symbol != "" {
			fmt.Fprintf(&sb, " :: %s", p.Symbol)
		}
		fmt.Fprintf(&sb, " (depth=%s)\n\n", depth)

		// --- File stats (always) ---
		info, err := os.Stat(filePath)
		if err != nil {
			return "", fmt.Errorf("file not found: %w", err)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		lineCount := len(strings.Split(string(data), "\n"))
		fmt.Fprintf(&sb, "## Stats\n- Size: %d bytes\n- Lines: %d\n- Modified: %s\n\n",
			info.Size(), lineCount, info.ModTime().Format("2006-01-02 15:04:05"))

		// --- Outline ---
		outlineInput, _ := json.Marshal(map[string]any{
			"action": "outline",
			"file":   p.File,
		})
		outlineResult, err := analyzeFn(ctx, outlineInput)
		if err != nil {
			fmt.Fprintf(&sb, "## Outline\n[Error: %s]\n\n", err.Error())
		} else {
			fmt.Fprintf(&sb, "## Outline\n%s\n", outlineResult)
		}

		// --- Imports ---
		importsInput, _ := json.Marshal(map[string]any{
			"action": "imports",
			"file":   p.File,
		})
		importsResult, err := analyzeFn(ctx, importsInput)
		if err != nil {
			fmt.Fprintf(&sb, "## Imports\n[Error: %s]\n\n", err.Error())
		} else {
			fmt.Fprintf(&sb, "## Imports\n%s\n", importsResult)
		}

		// --- Deep: git log ---
		if depth == "deep" || depth == "symbol" {
			gitLog := runGitCommand(ctx, defaultDir, "log", "--oneline", "-5", "--", filePath)
			fmt.Fprintf(&sb, "## Recent Git History\n```\n%s```\n\n", gitLog)
		}

		// --- Symbol-specific: definition + references + blame ---
		if depth == "symbol" && p.Symbol != "" {
			// Read the symbol definition.
			readFn := ToolRead(defaultDir)
			readInput, _ := json.Marshal(map[string]any{
				"file_path": p.File,
				"function":  p.Symbol,
			})
			readResult, err := readFn(ctx, readInput)
			if err != nil {
				fmt.Fprintf(&sb, "## Symbol Definition\n[Error: %s]\n\n", err.Error())
			} else {
				fmt.Fprintf(&sb, "## Symbol Definition\n%s\n", readResult)
			}

			// Find references.
			refsInput, _ := json.Marshal(map[string]any{
				"action": "references",
				"symbol": p.Symbol,
				"path":   filepath.Dir(p.File),
			})
			refsResult, err := analyzeFn(ctx, refsInput)
			if err != nil {
				fmt.Fprintf(&sb, "## References\n[Error: %s]\n\n", err.Error())
			} else {
				fmt.Fprintf(&sb, "## References\n%s\n", refsResult)
			}

			// Git blame for the symbol.
			blameOutput := runGitCommand(ctx, defaultDir,
				"log", "--oneline", "-3", "-S", p.Symbol, "--", filePath)
			if blameOutput != "" {
				fmt.Fprintf(&sb, "## Symbol Git History\n```\n%s```\n\n", blameOutput)
			}
		}

		return sb.String(), nil
	}
}

// runGitCommand runs a git command and returns stdout (or error message).
func runGitCommand(ctx context.Context, workDir string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("[git error: %s]\n", strings.TrimSpace(string(out)))
	}
	result := string(out)
	if result == "" {
		return "(no output)\n"
	}
	return result
}
