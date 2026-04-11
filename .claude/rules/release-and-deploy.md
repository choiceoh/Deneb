---
description: "릴리스, 어드바이저리, 프로덕션 배포 워크플로우"
globs: ["scripts/release*", "scripts/deploy*", ".github/workflows/release*"]
---

# Release & Advisory Workflows

- Release and publish remain explicit-approval actions.

# Production Deployment

## DGX Spark Production Build

- `make gateway-prod` — Full production binary (output: `dist/deneb-gateway`).

## DGX Spark Operations

- Restart gateway: `pkill -9 -f deneb-gateway || true; nohup ./gateway-go/deneb-gateway --bind loopback --port 18789 > /tmp/deneb-gateway.log 2>&1 &`
- Verify: `ss -ltnp | rg 18789`, `tail -n 120 /tmp/deneb-gateway.log`.
