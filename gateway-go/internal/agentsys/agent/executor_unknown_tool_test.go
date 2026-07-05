package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// unknownToolExecutor mimics chat.ToolRegistry.Execute's unknown-tool path:
// it wraps ErrUnknownTool the way unknownToolError does.
type unknownToolExecutor struct{}

func (unknownToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("%w: %q. Did you mean: grep?", ErrUnknownTool, name)
}

// A hallucinated tool name is tagged unknownTool in turn.tool and folds into
// the cross-session per-tool Unknown counter.
func TestExecuteOneTool_TagsUnknownToolInAgentlog(t *testing.T) {
	w := agentlog.NewWriter(t.TempDir())
	rl := agentlog.NewRunLogger(w, "client:test", "run1")

	tc := llm.ContentBlock{Type: "tool_use", ID: "t1", Name: "frobnicate", Input: json.RawMessage(`{}`)}
	block := executeOneTool(context.Background(), tc, unknownToolExecutor{}, StreamHooks{},
		"", 0, slog.Default(), rl, nil)
	if !block.IsError {
		t.Fatalf("expected error result, got %+v", block)
	}

	agg := w.Aggregate(0)
	for _, ts := range agg.Tools {
		if ts.Name == "frobnicate" {
			if ts.Unknown != 1 || ts.Errors != 1 {
				t.Fatalf("frobnicate stat = %+v, want unknown1/err1", ts)
			}
			return
		}
	}
	t.Fatalf("no frobnicate ToolStat in aggregate: %+v", agg.Tools)
}
