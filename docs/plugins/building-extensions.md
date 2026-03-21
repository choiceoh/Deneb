---
title: "Building Extensions"
summary: "Step-by-step guide for creating Deneb channel and provider extensions"
read_when:
  - You want to create a new Deneb plugin or extension
  - You need to understand the plugin SDK import patterns
  - You are adding a new channel or provider to Deneb
---

# Building Extensions

This guide walks through creating an Deneb extension from scratch. Extensions
can add channels, model providers, tools, or other capabilities.

## Prerequisites

- Deneb repository cloned and dependencies installed (`pnpm install`)
- Familiarity with TypeScript (ESM)

## Extension structure

Every extension lives under `extensions/<name>/` and follows this layout:

```
extensions/my-channel/
├── package.json          # npm metadata + deneb config
├── index.ts              # Entry point (defineChannelPluginEntry)
├── setup-entry.ts        # Setup wizard (optional)
├── api.ts                # Public contract barrel (optional)
├── runtime-api.ts        # Internal runtime barrel (optional)
└── src/
    ├── channel.ts        # Channel adapter implementation
    ├── runtime.ts        # Runtime wiring
    └── *.test.ts         # Colocated tests
```

## Step 1: Create the package

Create `extensions/my-channel/package.json`:

```json
{
  "name": "@deneb/my-channel",
  "version": "2026.1.1",
  "description": "Deneb My Channel plugin",
  "type": "module",
  "dependencies": {},
  "deneb": {
    "extensions": ["./index.ts"],
    "setupEntry": "./setup-entry.ts",
    "channel": {
      "id": "my-channel",
      "label": "My Channel",
      "selectionLabel": "My Channel (plugin)",
      "docsPath": "/channels/my-channel",
      "docsLabel": "my-channel",
      "blurb": "Short description of the channel.",
      "order": 80
    },
    "install": {
      "npmSpec": "@deneb/my-channel",
      "localPath": "extensions/my-channel"
    }
  }
}
```

The `deneb` field tells the plugin system what your extension provides.
For provider plugins, use `providers` instead of `channel`.

## Step 2: Define the entry point

Create `extensions/my-channel/index.ts`:

```typescript
import { defineChannelPluginEntry } from "deneb/plugin-sdk/core";

export default defineChannelPluginEntry({
  id: "my-channel",
  name: "My Channel",
  description: "Connects Deneb to My Channel",
  plugin: {
    // Channel adapter implementation
  },
});
```

For provider plugins, use `definePluginEntry` instead.

## Step 3: Import from focused subpaths

The plugin SDK exposes many focused subpaths. Always import from specific
subpaths rather than the monolithic root:

```typescript
// Correct: focused subpaths
import { defineChannelPluginEntry } from "deneb/plugin-sdk/core";
import { createChannelReplyPipeline } from "deneb/plugin-sdk/channel-reply-pipeline";
import { createChannelPairingController } from "deneb/plugin-sdk/channel-pairing";
import { createPluginRuntimeStore } from "deneb/plugin-sdk/runtime-store";
import { createOptionalChannelSetupSurface } from "deneb/plugin-sdk/channel-setup";
import { resolveChannelGroupRequireMention } from "deneb/plugin-sdk/channel-policy";

// Wrong: monolithic root (lint will reject this)
import { ... } from "deneb/plugin-sdk";
```

Common subpaths:

| Subpath                             | Purpose                              |
| ----------------------------------- | ------------------------------------ |
| `plugin-sdk/core`                   | Plugin entry definitions, base types |
| `plugin-sdk/channel-setup`          | Optional setup adapters/wizards      |
| `plugin-sdk/channel-pairing`        | DM pairing primitives                |
| `plugin-sdk/channel-reply-pipeline` | Prefix + typing reply wiring         |
| `plugin-sdk/channel-config-schema`  | Config schema builders               |
| `plugin-sdk/channel-policy`         | Group/DM policy helpers              |
| `plugin-sdk/secret-input`           | Secret input parsing/helpers         |
| `plugin-sdk/webhook-ingress`        | Webhook request/target helpers       |
| `plugin-sdk/runtime-store`          | Persistent plugin storage            |
| `plugin-sdk/allow-from`             | Allowlist resolution                 |
| `plugin-sdk/reply-payload`          | Message reply types                  |
| `plugin-sdk/provider-onboard`       | Provider onboarding config patches   |
| `plugin-sdk/testing`                | Test utilities                       |

Use the narrowest primitive that matches the job. Reach for `channel-runtime`
or other larger helper barrels only when a dedicated subpath does not exist yet.

## Step 4: Use local barrels for internal imports

Within your extension, create barrel files for internal code sharing instead
of importing through the plugin SDK:

```typescript
// api.ts — public contract for this extension
export { MyChannelConfig } from "./src/config.js";
export { MyChannelRuntime } from "./src/runtime.js";

// runtime-api.ts — internal-only exports (not for production consumers)
export { internalHelper } from "./src/helpers.js";
```

**Self-import guardrail**: never import your own extension back through its
published SDK contract path from production files. Route internal imports
through `./api.ts` or `./runtime-api.ts` instead. The SDK contract is for
external consumers only.

## Step 5: Add a plugin manifest

Create `deneb.plugin.json` in your extension root:

```json
{
  "id": "my-channel",
  "kind": "channel",
  "channels": ["my-channel"],
  "name": "My Channel Plugin",
  "description": "Connects Deneb to My Channel"
}
```

See [Plugin manifest](/plugins/manifest) for the full schema.

## Step 6: Test with contract tests

Deneb runs contract tests against all registered plugins. After adding your
extension, run:

```bash
pnpm test:contracts:channels   # channel plugins
pnpm test:contracts:plugins    # provider plugins
```

Contract tests verify your plugin conforms to the expected interface (setup
wizard, session binding, message handling, group policy, etc.).

For unit tests, import test helpers from the public testing surface:

```typescript
import { createTestRuntime } from "deneb/plugin-sdk/testing";
```

## Lint enforcement

Three scripts enforce SDK boundaries:

1. **No monolithic root imports** — `deneb/plugin-sdk` root is rejected
2. **No direct src/ imports** — extensions cannot import `../../src/` directly
3. **No self-imports** — extensions cannot import their own `plugin-sdk/<name>` subpath

Run `pnpm check` to verify all boundaries before committing.

## Checklist

Before submitting your extension:

- [ ] `package.json` has correct `deneb` metadata
- [ ] Entry point uses `defineChannelPluginEntry` or `definePluginEntry`
- [ ] All imports use focused `plugin-sdk/<subpath>` paths
- [ ] Internal imports use local barrels, not SDK self-imports
- [ ] `deneb.plugin.json` manifest is present and valid
- [ ] Contract tests pass (`pnpm test:contracts`)
- [ ] Unit tests colocated as `*.test.ts`
- [ ] `pnpm check` passes (lint + format)
- [ ] Doc page created under `docs/channels/` or `docs/plugins/`
