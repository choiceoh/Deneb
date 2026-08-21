# Browseops (browse)

Owns the resident headful-browser sidecar reader. Wiring stays in
`toolwire/webtools`. Screenshot delivery uses `tools/routine` visual dir.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/browseops`
