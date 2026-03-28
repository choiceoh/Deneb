package tools

import (
	"context"
	"encoding/json"
)

// ToolFunc executes a tool with JSON input.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)
