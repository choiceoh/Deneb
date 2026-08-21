# Messageops (message)

Owns the in-loop channel send/react tool. Wiring stays in
`toolwire/core` via `surface.ToolMessage`.

Must not import parent `tools` or `pipeline/chat`. Auto-delivery runs
return a benign skip, never a channel-down error.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/messageops`
