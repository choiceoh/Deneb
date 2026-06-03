---
description: "기계 생성 코드 수정 금지 규칙"
globs: ["gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go", "gateway-go/internal/pipeline/chat/tool_classification_gen.go", "client-android/app/composeApp/src/commonMain/kotlin/com/inspiredandroid/kai/deneb/generated/*.kt"]
---

# Generated Code Boundary

Several files in this repo are **machine-generated** and carry a `// Code generated ... DO NOT EDIT.` header. These files must never be edited by hand — not even for refactoring or style fixes.

| Generated file | Source of truth | Regenerate with |
|---|---|---|
| `gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go` | `gateway-go/internal/pipeline/chat/toolreg/tool_schemas.json` | `make tool-schemas` |
| `gateway-go/internal/pipeline/chat/tool_classification_gen.go` | `gateway-go/internal/pipeline/chat/tool_classification.json` | `make data-gen` |
| `client-android/.../deneb/generated/MiniappWireTypes.kt` | Go miniapp handler structs marked `//deneb:wire` (e.g. `gateway-go/internal/runtime/rpc/handler/handlerminiapp/calendar.go`) | `make kotlin-models` |

> The Kotlin wire types are generated from Go so the native client and the gateway share one source of truth for `miniapp.*` RPC response shapes. To extend coverage, add a `//deneb:wire` directive to another handler struct's doc comment and rerun `make kotlin-models` (referenced struct types are pulled in transitively).

## Rules

- To change a generated file, modify its source of truth, then run the corresponding `make` target.
- To change what a generator produces, modify the generator itself (`gateway-go/cmd/tool-schema-gen/main.go`, `gateway-go/cmd/data-gen/main.go`, `gateway-go/cmd/kotlin-models-gen/main.go`).
- CI enforces no-drift via `generate-check.yml`. Any PR that manually edits a generated file will fail CI.
- Do not mix hand-written and generated changes in the same commit.
