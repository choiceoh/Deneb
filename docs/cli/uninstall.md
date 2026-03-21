---
summary: "CLI reference for `deneb uninstall` (remove gateway service + local data)"
read_when:
  - You want to remove the gateway service and/or local state
  - You want a dry-run first
title: "uninstall"
---

# `deneb uninstall`

Uninstall the gateway service + local data (CLI remains).

```bash
deneb backup create
deneb uninstall
deneb uninstall --all --yes
deneb uninstall --dry-run
```

Run `deneb backup create` first if you want a restorable snapshot before removing state or workspaces.
