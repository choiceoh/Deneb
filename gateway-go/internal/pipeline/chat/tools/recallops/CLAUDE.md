# Recallops (knowledge / polaris / research_panel)

Owns session recall and knowledge-base tools. Wiring stays in
`toolwire/recall` and `surface.ToolResearchPanel`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/recallops`
