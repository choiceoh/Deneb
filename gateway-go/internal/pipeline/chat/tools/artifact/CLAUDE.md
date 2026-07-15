# Artifact / media tools map

Owns chat tools for charts, diagrams, OCR/ASR, spillover reads, watches, and
path guards. Consumes `toolport.ToolFunc` and `tooldeps` ports only.

## Entry points

- `chart.go` — `ToolChart`
- `diagram.go` — `ToolDiagram`
- `media_extract.go` — `ToolTranscribe`, `ToolOCR`
- `asr_export.go` — `TranscribeAudio`
- `spillover_read.go` — `ToolSpilloverRead`
- `watch.go` — `ToolWatch`
- `send_file.go` — `ToolSendFile`
- `path_guard.go` — `CheckProtectedPath`
- `path_resolve.go` — `ResolvePath`, `ResolvePathWithRoots`

## Dependency direction and invariants

- **Dependency / boundary**: must import `toolport`/`tooldeps` — never the
  chat parent package or `runtime/server`. Media backends stay behind these
  tool constructors.
- **Invariant**: `CheckProtectedPath` must block writes outside allowed
  roots; path resolution must never escape workspace roots; ASR/OCR tools
  must not panic on empty input.
- Prefer `tooldeps.SpilloverStore` ports over concrete domain types.

## Local change scope

Artifact tools stay in `tools/artifact`.

- May co-change: `toolwire` registration and `tooldeps` ports they need.
- Do not touch: recall preflight or wiki Store APIs directly.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/artifact
```
