# Deneb

Personal AI gateway for NVIDIA DGX Spark — a chief-of-staff-style single agent
that does deep business analysis (mail, projects, people, deals) and proactive
ops (calendar, meeting prep, capture) in one persona, on top of general
assistant capabilities. Korean-first, single-user, single-machine. Reachable
from the mobile native client (Android / iOS, one Kotlin Multiplatform
codebase) and the Andromeda desktop workstation (Tauri + React).

## Architecture

```
Native client (Android/iOS) ──┐
                              ├──> Go Gateway (HTTP + SSE)
Andromeda (desktop, Tauri) ───┘         │
                                    150+ RPC methods, 150+ agent tools
                                    Session management
                                    Chat/LLM pipeline
                                    Wiki knowledge base + Polaris session memory
                                    GPU sidecars (OCR, ASR, embeddings)
```

| Module | Language | Description |
|--------|----------|-------------|
| `gateway-go/` | Go | HTTP + SSE server, RPC dispatch (150+ methods), session management, chat/LLM pipeline, 150+ tool integrations |
| `client-android/` | Kotlin | Mobile native client (Kotlin Multiplatform: Android / iOS; vendored Kai UI, Apache-2.0) wired to the gateway over an authenticated `miniapp.*` RPC surface |
| `andromeda/` | TypeScript/Rust | Desktop workstation client (Tauri 2 + React 18 + Refine) on the same `miniapp.*` RPC + SSE surface |
| `skills/` | Markdown | Filesystem-discovered skill plugins by category |

## Prerequisites

- **Go** 1.25+
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

# Go-only check (fmt + vet + lint + test, plus generated-file drift)
make check

# Full pre-push gate (Go + native-client Kotlin lanes)
make ci

# Andromeda (separate lane)
cd andromeda && pnpm verify
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
