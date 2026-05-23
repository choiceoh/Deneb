# Deneb

Personal AI gateway for NVIDIA DGX Spark. Korean-first, single-user, Telegram-on-Android I/O. Specializes in business analysis (mail, projects, deals) on top of general assistant capabilities.

## Architecture

```
Telegram (Android) ──> Go Gateway (HTTP/WS)
                           │
                       130+ RPC methods
                       Session management
                       Chat/LLM pipeline
                       Telegram bot plugin
                       Wiki knowledge base + Polaris session memory
```

| Module | Language | Description |
|--------|----------|-------------|
| `gateway-go/` | Go | HTTP/WS server, RPC dispatch, session management, chat/LLM pipeline, 130+ tool integrations, Telegram bot |
| `proto/` | Protobuf | Cross-language type definitions (Go codegen) |

## Prerequisites

- **Go** 1.24+
- **buf** (latest) + protoc + protoc-gen-go
- **NVIDIA DGX Spark** for GPU inference (optional — CPU fallback available)

## Build

```bash
# Full build
make all

# Minimal
make go

# DGX Spark production
make gateway-dgx

# Development (auto-restart)
make go-dev
```

## Test

```bash
# All tests
make test

# All checks (fmt + lint + test)
make check
```

## Deploy

Single-machine deployment on DGX Spark:

```bash
git pull
make gateway-dgx
scripts/deploy/deploy.sh
```

## Documentation

Full docs at [docs.deneb.ai](https://docs.deneb.ai).

## License

[MIT](LICENSE)
