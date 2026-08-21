# Personaops (preference)

Owns append-only SOUL.md preference writes. Wiring stays in
`toolwire/domain.RegisterPersonaTools`.

Must not import parent `tools` or `pipeline/chat`.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/personaops`
