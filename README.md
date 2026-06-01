# Deneb

Personal AI gateway for NVIDIA DGX Spark — a chief-of-staff-style single agent
that does deep business analysis (mail, projects, people, deals) and proactive
ops (calendar, meeting prep, capture) in one persona, on top of general
assistant capabilities. Korean-first, single-user, single-machine. Reachable
from Telegram and a native Android client.

## Architecture

```
Telegram ─────────────┐
Native Android client ─┴──> Go Gateway (HTTP/WS)
                                 │
                             150+ RPC methods, 150+ agent tools
                             Session management
                             Chat/LLM pipeline
                             Telegram bot plugin
                             Wiki knowledge base + Polaris session memory
                             GPU sidecars (OCR, ASR, embeddings)
```

| Module | Language | Description |
|--------|----------|-------------|
| `gateway-go/` | Go | HTTP/WS server, RPC dispatch (150+ methods), session management, chat/LLM pipeline, 150+ tool integrations, Telegram bot |
| `client-android/` | Kotlin | Native Android client (vendored Kai UI, Apache-2.0) wired to the gateway over an authenticated endpoint |

## Prerequisites

- **Go** 1.24+
- **NVIDIA DGX Spark** for GPU inference (optional — CPU fallback available)

## Build

```bash
# Go gateway (default target)
make go

# DGX Spark production binary -> dist/deneb-gateway
make gateway-prod

# Development (auto-restart on SIGUSR1)
make go-dev
```

## Test

```bash
# Go tests
make test

# Full check (fmt + vet + lint + test, plus generated-file drift)
make check
```

## Deploy

Single-machine deployment on DGX Spark:

```bash
git pull
make gateway-prod
scripts/deploy/deploy.sh
```

## Documentation

Full docs at [docs.deneb.ai](https://docs.deneb.ai).

## License

[MIT](LICENSE). The native client under `client-android/app/` vendors
[Kai](https://github.com/SimonSchubert/Kai) and is Apache-2.0 — see
`client-android/NOTICE`.
