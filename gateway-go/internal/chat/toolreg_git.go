package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- Git tool ---
// Dedicated git operations with structured output.
// Complements the existing diff tool (which focuses on viewing diffs).

func toolGit(defaultDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p gitParams
		if err := jsonutil.UnmarshalInto("git params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "status":
			return gitStatus(ctx, defaultDir, p)
		case "commit":
			return gitCommit(ctx, defaultDir, p)
		case "log":
			return gitLog(ctx, defaultDir, p)
		case "branch":
			return gitBranch(ctx, defaultDir, p)
		case "stash":
			return gitStash(ctx, defaultDir, p)
		case "blame":
			return gitBlame(ctx, defaultDir, p)
		case "tag":
			return gitTag(ctx, defaultDir, p)
		case "merge":
			return gitMerge(ctx, defaultDir, p)
		case "rebase":
			return gitRebase(ctx, defaultDir, p)
		case "cherry_pick":
			return gitCherryPick(ctx, defaultDir, p)
		case "reset":
			return gitReset(ctx, defaultDir, p)
		case "remote":
			return gitRemote(ctx, defaultDir, p)
		case "clean":
			return gitClean(ctx, defaultDir, p)
		default:
			return "", fmt.Errorf("unknown git action: %q", p.Action)
		}
	}
}

type gitParams struct {
	Action       string   `json:"action"`
	Message      string   `json:"message"`
	Files        []string `json:"files"`
	All          bool     `json:"all"`
	Amend        bool     `json:"amend"`
	Count        int      `json:"count"`
	Oneline      bool     `json:"oneline"`
	Author       string   `json:"author"`
	Since        string   `json:"since"`
	Path         string   `json:"path"`
	Grep         string   `json:"grep"`
	Name         string   `json:"name"`
	Delete       bool     `json:"delete"`
	SwitchTo     bool     `json:"switch_to"`
	Create       bool     `json:"create"`
	From         string   `json:"from"`
	File         string   `json:"file"`
	StartLine    int      `json:"start_line"`
	EndLine      int      `json:"end_line"`
	StashAction  string   `json:"stash_action"`
	Branch       string   `json:"branch"`
	NoFF         bool     `json:"no_ff"`
	Abort        bool     `json:"abort"`
	ContinueOp   bool     `json:"continue_op"`
	Onto         string   `json:"onto"`
	Ref          string   `json:"ref"`
	Mode         string   `json:"mode"`
	RemoteAction string   `json:"remote_action"`
	URL          string   `json:"url"`
	DryRun       bool     `json:"dry_run"`
	Directories  bool     `json:"directories"`
	Force        bool     `json:"force"`
	Short        bool     `json:"short"`
}

// runGit executes a git command and returns its combined output.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		if result != "" {
			return result, nil
		}
		return "", fmt.Errorf("git %s failed: %w", args[0], err)
	}
	return result, nil
}

func gitStatus(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Short {
		return runGit(ctx, dir, "status", "--short", "--branch")
	}
	return runGit(ctx, dir, "status")
}

func gitCommit(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Message == "" && !p.Amend {
		return "", fmt.Errorf("message is required for commit (unless amending)")
	}

	// Stage specific files if provided.
	if len(p.Files) > 0 {
		args := append([]string{"add"}, p.Files...)
		if out, err := runGit(ctx, dir, args...); err != nil {
			return out, err
		}
	}

	args := []string{"commit"}
	if p.All {
		args = append(args, "-a")
	}
	if p.Amend {
		args = append(args, "--amend")
	}
	if p.Message != "" {
		args = append(args, "-m", p.Message)
	} else if p.Amend {
		args = append(args, "--no-edit")
	}

	return runGit(ctx, dir, args...)
}

func gitLog(ctx context.Context, dir string, p gitParams) (string, error) {
	count := p.Count
	if count <= 0 {
		count = 10
	}
	if count > 50 {
		count = 50
	}

	args := []string{"log", fmt.Sprintf("-n%d", count), "--no-color"}

	if p.Oneline {
		args = append(args, "--oneline")
	} else {
		args = append(args, "--format=%h %ad %an: %s", "--date=short")
	}
	if p.Author != "" {
		args = append(args, "--author="+p.Author)
	}
	if p.Since != "" {
		args = append(args, "--since="+p.Since)
	}
	if p.Grep != "" {
		args = append(args, "--grep="+p.Grep)
	}
	if p.Path != "" {
		args = append(args, "--", p.Path)
	}

	return runGit(ctx, dir, args...)
}

func gitBranch(ctx context.Context, dir string, p gitParams) (string, error) {
	// Switch to branch.
	if p.SwitchTo && p.Name != "" {
		return runGit(ctx, dir, "checkout", p.Name)
	}

	// Create branch.
	if p.Create && p.Name != "" {
		args := []string{"checkout", "-b", p.Name}
		if p.From != "" {
			args = append(args, p.From)
		}
		return runGit(ctx, dir, args...)
	}

	// Delete branch.
	if p.Delete && p.Name != "" {
		return runGit(ctx, dir, "branch", "-d", p.Name)
	}

	// List branches.
	return runGit(ctx, dir, "branch", "-a", "--no-color")
}

func gitStash(ctx context.Context, dir string, p gitParams) (string, error) {
	action := p.StashAction
	if action == "" {
		action = "list"
	}

	switch action {
	case "push":
		args := []string{"stash", "push"}
		if p.Message != "" {
			args = append(args, "-m", p.Message)
		}
		return runGit(ctx, dir, args...)
	case "pop":
		return runGit(ctx, dir, "stash", "pop")
	case "apply":
		return runGit(ctx, dir, "stash", "apply")
	case "drop":
		return runGit(ctx, dir, "stash", "drop")
	case "list":
		return runGit(ctx, dir, "stash", "list")
	default:
		return "", fmt.Errorf("unknown stash action: %q", action)
	}
}

func gitBlame(ctx context.Context, dir string, p gitParams) (string, error) {
	file := p.File
	if file == "" {
		file = p.Path
	}
	if file == "" {
		return "", fmt.Errorf("file is required for blame")
	}

	args := []string{"blame", "--no-color"}
	if p.StartLine > 0 && p.EndLine > 0 {
		args = append(args, fmt.Sprintf("-L%d,%d", p.StartLine, p.EndLine))
	} else if p.StartLine > 0 {
		args = append(args, fmt.Sprintf("-L%d,+20", p.StartLine))
	}
	args = append(args, file)

	return runGit(ctx, dir, args...)
}

func gitTag(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Name == "" {
		// List tags.
		return runGit(ctx, dir, "tag", "-l", "--sort=-creatordate")
	}

	if p.Delete {
		return runGit(ctx, dir, "tag", "-d", p.Name)
	}

	// Create tag.
	args := []string{"tag"}
	if p.Message != "" {
		args = append(args, "-a", p.Name, "-m", p.Message)
	} else {
		args = append(args, p.Name)
	}
	return runGit(ctx, dir, args...)
}

func gitMerge(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Abort {
		return runGit(ctx, dir, "merge", "--abort")
	}
	if p.Branch == "" {
		return "", fmt.Errorf("branch is required for merge")
	}
	args := []string{"merge"}
	if p.NoFF {
		args = append(args, "--no-ff")
	}
	args = append(args, p.Branch)
	return runGit(ctx, dir, args...)
}

func gitRebase(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Abort {
		return runGit(ctx, dir, "rebase", "--abort")
	}
	if p.ContinueOp {
		return runGit(ctx, dir, "rebase", "--continue")
	}
	if p.Onto == "" {
		return "", fmt.Errorf("onto is required for rebase")
	}
	return runGit(ctx, dir, "rebase", p.Onto)
}

func gitCherryPick(ctx context.Context, dir string, p gitParams) (string, error) {
	if p.Abort {
		return runGit(ctx, dir, "cherry-pick", "--abort")
	}
	if p.ContinueOp {
		return runGit(ctx, dir, "cherry-pick", "--continue")
	}
	if p.Ref == "" {
		return "", fmt.Errorf("ref is required for cherry-pick")
	}
	return runGit(ctx, dir, "cherry-pick", p.Ref)
}

func gitReset(ctx context.Context, dir string, p gitParams) (string, error) {
	ref := p.Ref
	if ref == "" {
		ref = "HEAD"
	}
	mode := p.Mode
	if mode == "" {
		mode = "mixed"
	}
	return runGit(ctx, dir, "reset", "--"+mode, ref)
}

func gitRemote(ctx context.Context, dir string, p gitParams) (string, error) {
	action := p.RemoteAction
	if action == "" {
		action = "list"
	}

	switch action {
	case "list":
		return runGit(ctx, dir, "remote", "-v")
	case "add":
		if p.Name == "" || p.URL == "" {
			return "", fmt.Errorf("name and url are required for remote add")
		}
		return runGit(ctx, dir, "remote", "add", p.Name, p.URL)
	case "remove":
		if p.Name == "" {
			return "", fmt.Errorf("name is required for remote remove")
		}
		return runGit(ctx, dir, "remote", "remove", p.Name)
	default:
		return "", fmt.Errorf("unknown remote action: %q", action)
	}
}

func gitClean(ctx context.Context, dir string, p gitParams) (string, error) {
	args := []string{"clean"}

	// Default to dry-run for safety.
	if p.DryRun || (!p.Force && !p.DryRun) {
		args = append(args, "-n")
	} else if p.Force {
		args = append(args, "-f")
	}

	if p.Directories {
		args = append(args, "-d")
	}

	return runGit(ctx, dir, args...)
}
