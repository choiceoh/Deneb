// blackboard.go — typed I/O board for multi-tool workflows.
//
// Replaces free-text intermediate summaries with named JSON keys and optional
// step contracts (plan → begin → work → end). Missing required inputs/outputs
// fail closed.
package workflowops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
)

// ToolBlackboard returns the `blackboard` tool.
func ToolBlackboard() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		board := toolport.BlackboardFromContext(ctx)
		if board == nil {
			return "", fmt.Errorf("blackboard: not available in this run")
		}
		var p struct {
			Action  string                     `json:"action"`
			Key     string                     `json:"key"`
			Value   json.RawMessage            `json:"value"`
			Keys    []string                   `json:"keys"`
			Step    string                     `json:"step"`
			Steps   []toolport.StepContract    `json:"steps"`
			Outputs map[string]json.RawMessage `json:"outputs"`
		}
		if err := jsonutil.UnmarshalInto("blackboard params", input, &p); err != nil {
			return "", err
		}
		action := strings.ToLower(strings.TrimSpace(p.Action))
		switch action {
		case "plan":
			if err := board.Plan(p.Steps); err != nil {
				return "", err
			}
			toolmeta.Set(ctx, "blackboardAction", "plan")
			toolmeta.Set(ctx, "blackboardSteps", len(p.Steps))
			return formatBoardPlan(board), nil

		case "begin":
			inputs, err := board.BeginStep(p.Step)
			if err != nil {
				return "", err
			}
			toolmeta.Set(ctx, "blackboardAction", "begin")
			toolmeta.Set(ctx, "blackboardStep", strings.TrimSpace(p.Step))
			return formatBoardBegin(p.Step, inputs), nil

		case "end":
			if err := board.EndStep(p.Step, p.Outputs); err != nil {
				return "", err
			}
			toolmeta.Set(ctx, "blackboardAction", "end")
			toolmeta.Set(ctx, "blackboardStep", strings.TrimSpace(p.Step))
			return formatBoardStatus(board, fmt.Sprintf("step %q closed", strings.TrimSpace(p.Step))), nil

		case "put":
			if err := board.Put(p.Key, p.Value, "put"); err != nil {
				return "", err
			}
			toolmeta.Set(ctx, "blackboardAction", "put")
			toolmeta.Set(ctx, "blackboardKey", strings.TrimSpace(p.Key))
			return formatBoardStatus(board, fmt.Sprintf("put %s", strings.TrimSpace(p.Key))), nil

		case "get":
			v, ok := board.Get(p.Key)
			if !ok {
				return "", fmt.Errorf("blackboard: key %q not found", strings.TrimSpace(p.Key))
			}
			toolmeta.Set(ctx, "blackboardAction", "get")
			return fmt.Sprintf("%s = %s", v.Key, string(v.Value)), nil

		case "require":
			vals, err := board.Require(p.Keys)
			if err != nil {
				return "", err
			}
			toolmeta.Set(ctx, "blackboardAction", "require")
			return formatBoardValues(vals), nil

		case "list", "status", "":
			toolmeta.Set(ctx, "blackboardAction", "list")
			return formatBoardStatus(board, ""), nil

		case "clear":
			board.Clear()
			toolmeta.Set(ctx, "blackboardAction", "clear")
			return "blackboard cleared", nil

		default:
			return "", fmt.Errorf("blackboard: unknown action %q (plan|begin|end|put|get|require|list|clear)", p.Action)
		}
	}
}

func formatBoardPlan(board *toolport.Blackboard) string {
	var b strings.Builder
	b.WriteString("blackboard plan:\n")
	for i, step := range board.Steps() {
		fmt.Fprintf(&b, "%d. %s", i+1, step.ID)
		if step.Goal != "" {
			fmt.Fprintf(&b, " — %s", step.Goal)
		}
		b.WriteByte('\n')
		if len(step.Inputs) > 0 {
			fmt.Fprintf(&b, "   in:  %s\n", strings.Join(step.Inputs, ", "))
		}
		if len(step.Outputs) > 0 {
			fmt.Fprintf(&b, "   out: %s\n", strings.Join(step.Outputs, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatBoardBegin(step string, inputs map[string]json.RawMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "blackboard begin %s\n", strings.TrimSpace(step))
	if len(inputs) == 0 {
		b.WriteString("inputs: (none)")
		return b.String()
	}
	b.WriteString(formatBoardValues(inputs))
	return b.String()
}

func formatBoardValues(vals map[string]json.RawMessage) string {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("values:\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s = %s\n", k, string(vals[k]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatBoardStatus(board *toolport.Blackboard, header string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header)
		b.WriteByte('\n')
	}
	if active := board.ActiveStep(); active != "" {
		fmt.Fprintf(&b, "active_step: %s\n", active)
	}
	if steps := board.Steps(); len(steps) > 0 {
		ids := make([]string, len(steps))
		for i, step := range steps {
			ids[i] = step.ID
		}
		fmt.Fprintf(&b, "plan: %s\n", strings.Join(ids, " → "))
	}
	vals := board.List()
	if len(vals) == 0 {
		b.WriteString("values: (empty)")
		return b.String()
	}
	b.WriteString("values:\n")
	for _, v := range vals {
		fmt.Fprintf(&b, "- %s = %s", v.Key, string(v.Value))
		if v.Source != "" {
			fmt.Fprintf(&b, "  [%s]", v.Source)
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
