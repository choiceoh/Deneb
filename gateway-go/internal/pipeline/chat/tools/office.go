package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// officeCommands whitelists the officecli subcommands the agent may drive.
// Excluded on purpose:
//   - resident-lifecycle verbs (open/close/save/watch/unwatch): this tool is
//     stateless per call, so each standalone command runs its own
//     open→edit→flush→close cycle and the change is already on disk when it
//     returns — no resident to manage.
//   - agent-integration verbs (plugins/mcp/skills/install): host setup, not
//     document work.
var officeCommands = map[string]struct{}{
	"create": {}, "view": {}, "get": {}, "query": {},
	"set": {}, "add": {}, "remove": {}, "move": {}, "swap": {},
	"import": {}, "merge": {}, "batch": {}, "dump": {},
	"validate": {}, "refresh": {}, "raw": {}, "raw-set": {}, "add-part": {},
	"help": {},
}

// ToolOffice wraps the `officecli` binary so the agent can read and edit Office
// documents (.docx/.xlsx/.pptx) without any Office install. Grammar:
//
//	officecli <command> <file> [args...] --json
//
// It shells out through an argument array (no shell → no injection) with
// officecli's network auto-update disabled. Standalone commands flush to disk
// themselves, so a later read/download of <file> already sees the edits.
// Output is bounded by the executor (tool_schemas.json max_output + spillover).
func ToolOffice(workspaceDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Command string   `json:"command"`
			File    string   `json:"file"`
			Args    []string `json:"args"`
			Stdin   string   `json:"stdin"`
		}
		if err := jsonutil.UnmarshalInto("office params", input, &p); err != nil {
			return "", err
		}
		p.Command = strings.TrimSpace(p.Command)
		if p.Command == "" {
			return "", fmt.Errorf("command is required")
		}
		if _, ok := officeCommands[p.Command]; !ok {
			return "", fmt.Errorf("unsupported office command %q (allowed: %s)", p.Command, officeCommandList())
		}
		// `help` prints officecli's schema reference and takes no <file> operand
		// (e.g. `help xlsx set`); every other command needs a document path.
		file := strings.TrimSpace(p.File)
		if p.Command != "help" && file == "" {
			return "", fmt.Errorf("file is required for office command %q", p.Command)
		}

		argv := []string{p.Command}
		if file != "" {
			// Relative paths resolve against the workspace; absolute paths pass
			// through so the agent can operate on the operator's own documents.
			if !filepath.IsAbs(file) {
				file = filepath.Join(workspaceDir, file)
			}
			// Prompt-injection safeguard: the agent may touch documents anywhere,
			// but never credential / control-plane files (~/.ssh, ~/.deneb/*.env,
			// id_rsa, …) — officecli's set/import/raw-set can overwrite arbitrary
			// paths. Same hard-deny the fs read/write/edit tools apply.
			if err := artifact.CheckProtectedPath(file, "access"); err != nil {
				return "", err
			}
			argv = append(argv, file)
		}
		argv = append(argv, p.Args...)
		argv = append(argv, "--json")

		cmd := exec.CommandContext(ctx, "officecli", argv...)
		cmd.Dir = workspaceDir
		cmd.Env = append(os.Environ(), "OFFICECLI_SKIP_UPDATE=1")
		if p.Stdin != "" {
			cmd.Stdin = strings.NewReader(p.Stdin)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// officecli reports usage/path/selector errors on stderr with a
			// non-zero exit. Surface them as a normal result (nil error) so the
			// model can correct the argument instead of aborting the turn.
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = strings.TrimSpace(stdout.String())
			}
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Sprintf("officecli %s 실패: %s", p.Command, msg), nil
		}
		out := stdout.String()
		if strings.TrimSpace(out) == "" {
			out = stderr.String()
		}
		return out, nil
	}
}

// officeCommandList renders the allowed commands for error messages.
func officeCommandList() string {
	names := make([]string, 0, len(officeCommands))
	for k := range officeCommands {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
