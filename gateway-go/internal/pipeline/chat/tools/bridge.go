package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// BroadcastFunc broadcasts an event to WebSocket clients.
// Defined here to avoid importing rpcutil (which would create an import cycle).
type BroadcastFunc func(event string, payload any) (int, []error)

// ToolBridge returns a tool that sends messages to other agents via the
// inter-agent bridge. The main agent uses this to communicate with
// Claude Code and other external agents.
func ToolBridge(broadcaster BroadcastFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Message string `json:"message"`
			Source  string `json:"source"`
		}
		if err := jsonutil.UnmarshalInto("bridge params", input, &p); err != nil {
			return "", err
		}
		if p.Message == "" {
			return "", fmt.Errorf("message is required")
		}
		if p.Source == "" {
			p.Source = "deneb"
		}

		payload := map[string]any{
			"message": p.Message,
			"source":  p.Source,
		}
		sent, _ := broadcaster("bridge.message", payload)

		return fmt.Sprintf("Bridge message sent (delivered to %d listeners)", sent), nil
	}
}
