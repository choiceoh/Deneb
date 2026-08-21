package graphifyops

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

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolGraphify wraps the `graphify` CLI so the agent can query the wiki
// knowledge graph at ~/.deneb/wiki-graph/graph.json (built by the wiki
// dreamer each cycle). A custom graph path may also be passed.
func ToolGraphify(workspaceDir string) toolport.ToolFunc {
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
		// Expected state, not a hard failure: the graph simply hasn't been
		// built yet (rebuilds each wiki-dream cycle). Guide the model to the
		// wiki tool instead of surfacing an error to the user.
		if _, err := os.Stat(graphPath); errors.Is(err, fs.ErrNotExist) {
			return fmt.Sprintf("그래프가 아직 없습니다 (%s) — wiki-dream 사이클마다 자동 재구축됩니다. 지금은 `wiki(action=\"search\")` 또는 `knowledge(op=\"recall\")`로 같은 질문에 답하세요.", graphPath), nil
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
			return fmt.Sprintf("graphify %s 실행 실패: %s — 그래프 CLI 문제일 수 있으니 `wiki(action=\"search\")`로 대신 답하세요.", p.Action, msg), nil
		}
		out := stdout.String()
		if strings.TrimSpace(out) == "" {
			out = stderr.String()
		}
		return out, nil
	}
}

// resolveGraphifyPath resolves the user-supplied graph hint to an absolute path.
// Accepts: "" (defaults to wiki), "wiki", "code" (the workspace code graph at
// workspace/graphify-out/graph.json), a relative path (resolved from
// workspaceDir), or an absolute path. The wiki graph follows the graphify CLI
// convention of <root>/graphify-out/graph.json.
func resolveGraphifyPath(hint, workspaceDir string) (string, error) {
	switch hint {
	case "", "wiki":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for wiki graph: %w", err)
		}
		return filepath.Join(home, ".deneb", "wiki-graph", "graphify-out", "graph.json"), nil
	case "code":
		// Code call/import graph built by `graphify update .` in the workspace.
		// Matches the system prompt's graph="code" guidance.
		return filepath.Join(workspaceDir, "graphify-out", "graph.json"), nil
	}
	if filepath.IsAbs(hint) {
		return hint, nil
	}
	return filepath.Join(workspaceDir, hint), nil
}
