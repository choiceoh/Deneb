---
description: "기계 생성 코드 수정 금지 규칙"
globs: ["gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go", "gateway-go/internal/pipeline/autoreply/thinking/model_caps_gen.go", "gateway-go/internal/pipeline/chat/tool_classification_gen.go"]
---

# Generated Code Boundary

Several Go files in this repo are **machine-generated** and carry a `// Code generated ... DO NOT EDIT.` header. These files must never be edited by hand — not even for refactoring or style fixes.

| Generated file | Source of truth | Regenerate with |
|---|---|---|
| `gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go` | `gateway-go/internal/pipeline/chat/toolreg/tool_schemas.json` | `make tool-schemas` |
| `gateway-go/internal/pipeline/autoreply/thinking/model_caps_gen.go` | `gateway-go/internal/pipeline/autoreply/thinking/model_caps.yaml` | `make model-caps` |
| `gateway-go/internal/pipeline/chat/tool_classification_gen.go` | `gateway-go/internal/pipeline/chat/tool_classification.yaml` | `make data-gen` |

## Rules

- To change a generated file, modify its source of truth, then run the corresponding `make` target.
- To change what a generator produces, modify the generator itself (`gateway-go/cmd/tool-schema-gen/main.go`, `gateway-go/cmd/model-caps-gen/main.go`, `gateway-go/cmd/data-gen/main.go`).
- CI enforces no-drift via `generate-check.yml`. Any PR that manually edits a generated file will fail CI.
- Do not mix hand-written and generated changes in the same commit.
