---
title: Generated Artifacts
summary: Auto-generated baseline configs and channel metadata from the Deneb schema.
read_when: ["Understanding the config schema", "Reviewing plugin metadata", "Validating configuration baselines"]
---

# Generated Docs Artifacts

These baseline artifacts are generated from the repo-owned Deneb config schema and bundled channel/plugin metadata.

- Do not edit `config-baseline.json` by hand.
- Do not edit `config-baseline.jsonl` by hand.
- Regenerate it with `pnpm config:docs:gen`.
- Validate it in CI or locally with `pnpm config:docs:check`.
