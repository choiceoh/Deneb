---
description: "기계 생성 코드 수정 금지 규칙"
globs: ["gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go", "gateway-go/internal/pipeline/chat/tool_classification_gen.go", "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/generated/*.kt"]
---

# Generated Code Boundary

Several files in this repo are **machine-generated** and carry a `// Code generated ... DO NOT EDIT.` header. These files must never be edited by hand — not even for refactoring or style fixes.

| Generated file | Source of truth | Regenerate with |
|---|---|---|
| `gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go` | `gateway-go/internal/pipeline/chat/toolreg/tool_schemas.json` | `make tool-schemas` |
| `gateway-go/internal/pipeline/chat/tool_classification_gen.go` | `gateway-go/internal/pipeline/chat/tool_classification.json` | `make data-gen` |
| `client-android/.../deneb/generated/MiniappWireTypes.kt` | Go miniapp handler structs marked `//deneb:wire` (e.g. `gateway-go/internal/runtime/rpc/handler/handlerminiapp/calendar.go`) | `make kotlin-models` |
| `andromeda/src/gen/miniappWire.ts` | the **same** `//deneb:wire` structs (Andromeda desktop client) | `pnpm gen:wire` (from `andromeda/`) |

> The Kotlin **and** TypeScript wire types are generated from the same Go `//deneb:wire` structs, so the native client (Kotlin), the Andromeda desktop client (TS), and the gateway share one source of truth for `miniapp.*` RPC response shapes. **A change to any `//deneb:wire` struct must regenerate BOTH** (`make kotlin-models` *and* `pnpm gen:wire`) or the `wire-drift` CI check fails. To extend coverage, add a `//deneb:wire` directive to another handler struct's doc comment and rerun both (referenced struct types are pulled in transitively).

## Rules

- To change a generated file, modify its source of truth, then run the corresponding `make` target.
- To change what a generator produces, modify the generator itself (`gateway-go/cmd/tool-schema-gen/main.go`, `gateway-go/cmd/data-gen/main.go`, `gateway-go/cmd/kotlin-models-gen/main.go`, `gateway-go/cmd/ts-models-gen/main.go`).
- CI enforces no-drift via `generate-check.yml`. Any PR that manually edits a generated file will fail CI.
- Do not mix hand-written and generated changes in the same commit.
