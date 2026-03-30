# Deneb Codebase Patterns

**Always reuse existing code - no redundancy!**

## Tech Stack

- **Languages**: Go (gateway), Rust (core library, CLI)
- **Build**: Makefile orchestrates Rust (`cargo`) and Go (`go build`)
- **Tests**: `cargo test` (Rust), `go test` (Go)
- **Protobuf**: buf + prost (Rust) + protoc-gen-go (Go)

## Anti-Redundancy Rules

- Avoid files that just re-export from another file. Import directly from the original source.
- If a function already exists, import it - do NOT create a duplicate in another file.
- Before creating any utility or helper, search for existing implementations first.

## Source of Truth Locations

### Go Gateway (`gateway-go/`)

- RPC methods: `internal/rpc/`
- Session management: `internal/session/`
- Channel plugins: `internal/channel/`
- HTTP server: `internal/server/`
- Chat/LLM: `internal/chat/`

### Rust Core (`core-rs/`)

- Protocol validation: `core/src/protocol/`
- Security: `core/src/security/`
- Media detection: `core/src/media/`
- FFI entry: `core/src/lib.rs`

## Code Quality

- Colocated tests: `*_test.go` (Go), `#[cfg(test)]` (Rust)
- Run `make check` before commits
- Run `make test` for full test suite
- When shell inspection is needed, prefer high-performance CLIs: `rg`, `fd`, `bat`, `eza`, `sd`, `dust`, `duf`, `procs`, `fx`, `ouch`, `btm`

## Commands

- **Build**: `make all` (Rust + Go)
- **Test**: `make test` (Rust + Go tests)
- **Check**: `make check` (proto-check + rust-test + go-test)
- **Rust only**: `make rust` / `make rust-test`
- **Go only**: `make go` / `make go-test`
- **Proto**: `make proto` / `make proto-check`
