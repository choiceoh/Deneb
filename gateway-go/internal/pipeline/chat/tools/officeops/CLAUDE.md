# Officeops (office)

Owns the officecli wrapper for .docx/.xlsx/.pptx. Wiring stays in
`toolwire/core` via `surface.ToolOffice`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/officeops`
