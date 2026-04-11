package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Diff tool ---
// Shows git diff, file comparison, or uncommitted changes.
// Coding agents need diff to review changes before committing.

func ToolDiff(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Mode         string `json:"mode"`
			Path         string `json:"path"`
			Ref          string `json:"ref"`
			Ref2         string `json:"ref2"`
			StatOnly     bool   `json:"stat_only"`
			ContextLines int    `json:"context_lines"`
		}
		if err := jsonutil.UnmarshalInto("diff params", input, &p); err != nil {
			return "", err
		}
		if p.Mode == "" {
			p.Mode = "unstaged"
		}

		// Handle file-to-file comparison separately (no git needed).
		if p.Mode == "files" {
			return diffFiles(p.Path, p.Ref2, defaultDir)
		}

		// Build git diff command.
		args := []string{"diff", "--no-color"}

		// Context lines.
		contextLines := p.ContextLines
		if contextLines < 0 {
			contextLines = 0
		}
		if contextLines > 20 {
			contextLines = 20
		}
		if contextLines != 3 {
			args = append(args, fmt.Sprintf("-U%d", contextLines))
		}

		if p.StatOnly {
			args = append(args, "--stat")
		}

		switch p.Mode {
		case "staged":
			args = append(args, "--cached")
		case "unstaged":
			// default git diff (working tree vs index)
		case "all":
			args = append(args, "HEAD")
		case "commit":
			ref := p.Ref
			if ref == "" {
				ref = "HEAD"
			}
			// Show the diff introduced by a specific commit.
			args = []string{"show", "--no-color", "--format=commit %H%nAuthor: %an <%ae>%nDate: %ad%nSubject: %s%n", ref}
			if p.StatOnly {
				args = append(args, "--stat")
			}
		case "branch":
			if p.Ref == "" {
				return "", fmt.Errorf("ref is required for branch mode (base branch)")
			}
			ref2 := p.Ref2
			if ref2 == "" {
				ref2 = "HEAD"
			}
			args = append(args, fmt.Sprintf("%s...%s", p.Ref, ref2))
		default:
			return "", fmt.Errorf("unknown diff mode: %q", p.Mode)
		}

		// Add path filter.
		if p.Path != "" && p.Mode != "commit" {
			args = append(args, "--", p.Path)
		}

		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = defaultDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			// git diff exits 1 when there are differences in some modes,
			// but that's not an error. Only report actual failures.
			if len(out) > 0 {
				return string(out), nil
			}
			return "", fmt.Errorf("git diff failed: %w", err)
		}

		result := strings.TrimSpace(string(out))
		if result == "" {
			return "No differences found.", nil
		}

		// Truncate very large diffs to avoid blowing up context.
		const maxDiffLen = 64000
		if len(result) > maxDiffLen {
			result = result[:maxDiffLen] + fmt.Sprintf("\n\n[... truncated, %d total chars. Use path filter or stat_only to narrow.]", len(result))
		}

		return result, nil
	}
}

// diffFiles compares two files using the system diff command.
func diffFiles(file1, file2, defaultDir string) (string, error) {
	if file1 == "" || file2 == "" {
		return "", fmt.Errorf("files mode requires path (first file) and ref2 (second file)")
	}

	path1 := ResolvePath(file1, defaultDir)
	path2 := ResolvePath(file2, defaultDir)

	cmd := exec.CommandContext(context.Background(), "diff", "-u", "--color=never", path1, path2)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// diff exits 1 when files differ — that's expected.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return string(out), nil
		}
		if len(out) > 0 {
			return string(out), nil
		}
		return "", fmt.Errorf("diff failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "Files are identical.", nil
	}
	return result, nil
}
