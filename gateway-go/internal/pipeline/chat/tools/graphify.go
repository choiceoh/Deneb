package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolGraphify wraps the `graphify` CLI so the agent can query knowledge
// graphs at graphify-out/graph.json (code) or ~/.deneb/wiki-graph/graph.json
// (wiki, built by the wiki dreamer). Build/update with `graphify update .`
// for code; the wiki graph rebuilds automatically each dream cycle.
func ToolGraphify(workspaceDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action   string `json:"action"`
			Question string `json:"question"`
			Node     string `json:"node"`
			From     string `json:"from"`
			To       string `json:"to"`
			Budget   int    `json:"budget"`
			DFS      bool   `json:"dfs"`
			Graph    string `json:"graph"`
		}
		if err := jsonutil.UnmarshalInto("graphify params", input, &p); err != nil {
			return "", err
		}

		graphPath, err := resolveGraphifyPath(p.Graph, workspaceDir)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(graphPath); errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("graph not found at %s — for the code graph run `graphify update .` in the workspace; the wiki graph rebuilds automatically each wiki-dream cycle", graphPath)
		}

		var args []string
		switch p.Action {
		case "query":
			if p.Question == "" {
				return "", fmt.Errorf("question is required for action=query")
			}
			args = []string{"query", p.Question, "--graph", graphPath}
			if p.Budget > 0 {
				args = append(args, "--budget", strconv.Itoa(p.Budget))
			}
			if p.DFS {
				args = append(args, "--dfs")
			}
		case "explain":
			if p.Node == "" {
				return "", fmt.Errorf("node is required for action=explain")
			}
			args = []string{"explain", p.Node, "--graph", graphPath}
		case "path":
			if p.From == "" || p.To == "" {
				return "", fmt.Errorf("from and to are required for action=path")
			}
			args = []string{"path", p.From, p.To, "--graph", graphPath}
		default:
			return "", fmt.Errorf("unknown graphify action: %q (expected query|path|explain)", p.Action)
		}

		cmd := exec.CommandContext(ctx, "graphify", args...)
		cmd.Dir = workspaceDir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return "", fmt.Errorf("graphify %s failed: %s", p.Action, msg)
		}
		out := stdout.String()
		if strings.TrimSpace(out) == "" {
			out = stderr.String()
		}
		return out, nil
	}
}

// resolveGraphifyPath resolves the user-supplied graph hint to an absolute path.
// Accepts: "" (defaults to wiki), "wiki", "code", a relative path (resolved
// from workspaceDir), or an absolute path. Both built-in graphs follow the
// graphify CLI convention of <root>/graphify-out/graph.json.
func resolveGraphifyPath(hint, workspaceDir string) (string, error) {
	switch hint {
	case "", "wiki":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for wiki graph: %w", err)
		}
		return filepath.Join(home, ".deneb", "wiki-graph", "graphify-out", "graph.json"), nil
	case "code":
		return filepath.Join(workspaceDir, "graphify-out", "graph.json"), nil
	}
	if filepath.IsAbs(hint) {
		return hint, nil
	}
	return filepath.Join(workspaceDir, hint), nil
}
