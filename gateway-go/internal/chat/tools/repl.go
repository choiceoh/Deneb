package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
)

// ToolREPL returns a tool that executes Starlark code in the RLM REPL.
// The REPL environment is stored per-request in the context via repl.WithEnv().
// Variables persist across tool calls within the same agent run.
func ToolREPL() toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(p.Code) == "" {
			return "code는 필수입니다.", nil
		}

		env := repl.FromContext(ctx)
		if env == nil {
			return "REPL 환경이 초기화되지 않았습니다.", nil
		}

		result := env.Execute(p.Code)

		var out strings.Builder
		if result.Stdout != "" {
			out.WriteString(result.Stdout)
		}
		if result.Error != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString("Error: ")
			out.WriteString(result.Error)
		}
		if result.FinalAnswer != nil {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString("FINAL: ")
			out.WriteString(*result.FinalAnswer)
		}
		if out.Len() == 0 {
			return "(no output)", nil
		}
		return out.String(), nil
	}
}
