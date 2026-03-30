package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

func ToolAgentLogs(w *agentlog.Writer) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if w == nil {
			return "", fmt.Errorf("agent logs not available")
		}

		var p struct {
			RunID string `json:"run_id"`
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("agent_logs params", input, &p); err != nil {
			return "", err
		}

		sessionKey := toolctx.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "", fmt.Errorf("no session context available")
		}

		result := w.Read(agentlog.ReadOpts{
			SessionKey: sessionKey,
			RunID:      p.RunID,
			Type:       p.Type,
			Limit:      p.Limit,
		})

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal result: %w", err)
		}
		return string(out), nil
	}
}
