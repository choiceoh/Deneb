# Graphifyops (graphify)

Owns the wiki-graph CLI wrapper. Wiring stays in `toolwire/core` via
`surface.ToolGraphify`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/graphifyops`
