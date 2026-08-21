# Translateops (DeepL segments)

Owns `TranslateSegments` for in-app page translation. Wiring stays in
`runtime/server/toolbind/docmedia`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/translateops`
