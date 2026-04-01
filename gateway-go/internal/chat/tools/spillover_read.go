package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolSpilloverRead returns a ToolFunc that reads the full content of a
// previously spilled large tool result by its spill ID.
// Access is session-scoped: the caller must belong to the same session.
func ToolSpilloverRead(store *agent.SpilloverStore) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SpillID string `json:"spill_id"`
		}
		if err := jsonutil.UnmarshalInto("read_spillover params", input, &p); err != nil {
			return "", err
		}
		if p.SpillID == "" {
			return "", fmt.Errorf("spill_id is required")
		}

		sessionKey := toolctx.SessionKeyFromContext(ctx)
		content, err := store.Load(p.SpillID, sessionKey)
		if err != nil {
			return "", fmt.Errorf("read_spillover: %w", err)
		}
		return content, nil
	}
}
