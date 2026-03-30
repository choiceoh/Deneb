package chat

import (
    "context"
    "encoding/json"
)

type ToolExecutor interface {
    Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)
