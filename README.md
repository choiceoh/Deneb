# Deneb

Personal AI gateway for NVIDIA DGX Spark. Telegram bot interface, Go gateway server, and Rust core engine — single-user, single-machine deployment.

## Architecture

```
Telegram (Android) ──> Go Gateway (HTTP/WS) ──> Rust Core (FFI)
                           │                        │
                       130+ RPC methods         Protocol validation
                       Session management       SIMD memory search
                       Chat/LLM pipeline        Media processing
                       Telegram bot plugin      Context engine
                                                Vega semantic search
                                                GGUF inference (CUDA)
```

| Module | Language | Description |
|--------|----------|-------------|
| `gateway-go/` | Go | HTTP/WS server, RPC dispatch, session management, chat/LLM pipeline, 130+ tool integrations, Telegram bot |
| `core-rs/` | Rust | Protocol validation, security, media processing, memory search (SIMD cosine + BM25 + FTS5), context engine, Vega semantic search, GGUF inference |
| `proto/` | Protobuf | Cross-language type definitions (Go + Rust codegen) |
| `cli-rs/` | Rust | CLI entry point, connects to gateway via WebSocket |

**Build order:** Proto schemas -> Rust core (static lib) -> Go gateway (links Rust via CGo)

## Prerequisites

- **Rust** (stable, via rustup)
- **Go** 1.24+
- **buf** (latest) + protoc + protoc-gen-go
- **NVIDIA DGX Spark** for GPU inference (optional — CPU fallback available)

## Build

```bash
# Full build (Rust + Go + CLI)
make all

# Minimal (no GPU)
make rust && make go

# DGX Spark production (Vega + ML + CUDA)
make gateway-dgx

# Development (debug Rust + auto-restart Go)
make rust-debug && make go-dev
```

## Test

```bash
# All tests (Rust + Go + CLI)
make test

# All checks (fmt + lint + test)
make check
```

## Deploy

Single-machine deployment on DGX Spark:

```bash
git pull
make gateway-dgx
scripts/deploy.sh
```

## Documentation

Full docs at [docs.deneb.ai](https://docs.deneb.ai).

## License

[MIT](LICENSE)
