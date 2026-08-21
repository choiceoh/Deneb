# Orgops (org)

Owns read-only org-chart queries. Wiring stays in `toolwire/core` via
`surface.ToolOrg`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/orgops`
