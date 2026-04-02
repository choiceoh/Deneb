---
description: "기계 생성 코드 수정 금지 규칙"
globs: ["gateway-go/internal/chat/toolreg/tool_schemas_gen.go", "gateway-go/internal/autoreply/thinking/model_caps_gen.go", "gateway-go/internal/ffi/ffi_error_codes_gen.go", "gateway-go/pkg/protocol/errors_gen.go", "core-rs/core/src/protocol/error_codes.rs", "gateway-go/pkg/protocol/gen/*.pb.go"]
---

# Generated Code Boundary

Several Go files in this repo are **machine-generated** and carry a `// Code generated ... DO NOT EDIT.` header. These files must never be edited by hand — not even for refactoring or style fixes.

| Generated file | Source of truth | Regenerate with |
|---|---|---|
| `gateway-go/internal/chat/toolreg/tool_schemas_gen.go` | `gateway-go/internal/chat/toolreg/tool_schemas.yaml` | `make tool-schemas` |
| `gateway-go/internal/autoreply/thinking/model_caps_gen.go` | `gateway-go/internal/autoreply/thinking/model_caps.yaml` | `make model-caps` |
| `gateway-go/internal/ffi/ffi_error_codes_gen.go` | `proto/gateway.proto` (FfiErrorCode) | `make error-codes-gen` |
| `gateway-go/pkg/protocol/errors_gen.go` | `proto/gateway.proto` (ErrorCode) | `make error-codes-gen` |
| `core-rs/core/src/protocol/error_codes.rs` | `proto/gateway.proto` (ErrorCode + FfiErrorCode) | `make error-codes-gen` |
| `gateway-go/pkg/protocol/gen/*.pb.go` | `proto/*.proto` | `make proto` |

## Rules

- To change a generated file, modify its source of truth, then run the corresponding `make` target.
- To change what a generator produces, modify the generator itself (`gateway-go/cmd/tool-schema-gen/gen.py`, `gateway-go/cmd/model-caps-gen/gen.py`, `scripts/gen-error-codes.sh`, etc.).
- CI enforces no-drift via `generate-check.yml` (non-proto generators) and `proto-check.yml` (proto generators). Any PR that manually edits a generated file will fail CI.
- Do not mix hand-written and generated changes in the same commit.
