---
summary: "CLI reference for `deneb reset` (reset local state/config)"
read_when:
  - You want to wipe local state while keeping the CLI installed
  - You want a dry-run of what would be removed
title: "reset"
---

# `deneb reset`

Reset local config/state (keeps the CLI installed).

```bash
deneb backup create
deneb reset
deneb reset --dry-run
deneb reset --scope config+creds+sessions --yes --non-interactive
```

Run `deneb backup create` first if you want a restorable snapshot before removing local state.
