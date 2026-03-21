---
summary: "CLI reference for `deneb logs` (tail gateway logs via RPC)"
read_when:
  - You need to tail Gateway logs remotely (without SSH)
  - You want JSON log lines for tooling
title: "logs"
---

# `deneb logs`

Tail Gateway file logs over RPC (works in remote mode).

Related:

- Logging overview: [Logging](/logging)

## Examples

```bash
deneb logs
deneb logs --follow
deneb logs --json
deneb logs --limit 500
deneb logs --local-time
deneb logs --follow --local-time
```

Use `--local-time` to render timestamps in your local timezone.
