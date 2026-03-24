---
title: "Configuration Guidelines"
summary: "Best practices and conventions for adding, modifying, and maintaining Deneb config fields"
read_when:
  - Adding a new config section or field to Deneb
  - Modifying the config schema or validation logic
  - Reviewing config-related pull requests
---

# Configuration Guidelines

This page covers the conventions and best practices for working with the Deneb configuration system. Follow these guidelines when adding new fields, modifying schemas, or updating config-related code.

For user-facing config docs, see [Configuration](/gateway/configuration). For the full field reference, see [Configuration Reference](/gateway/configuration-reference).

## Architecture overview

The config system has four layers:

| Layer        | Location                     | Purpose                     |
| ------------ | ---------------------------- | --------------------------- |
| **Types**    | `src/config/types.*.ts`      | TypeScript type definitions |
| **Schemas**  | `src/config/zod-schema.*.ts` | Zod validation schemas      |
| **Defaults** | `src/config/defaults.ts`     | Runtime default application |
| **I/O**      | `src/config/io*.ts`          | Read, write, cache, reload  |

All layers must stay in sync when adding or changing config fields.

## Adding a new config field

<Steps>
  <Step title="Define the type">
    Add the field to the appropriate `types.*.ts` file. Use the existing type file that matches the config section (for example `types.agents.ts` for agent config, `types.gateway.ts` for gateway config).

    ```typescript
    // src/config/types.agents.ts
    export type AgentDefaultsConfig = {
      // ...existing fields...
      /** Maximum concurrent tool calls per session. Default: 4. */
      maxConcurrentTools?: number;
    };
    ```

    Rules:
    - All config fields must be **optional** (`?:`) — Deneb uses safe defaults when omitted
    - Add a JSDoc comment with a brief description and the default value
    - Use strict types — avoid `any`, prefer unions, literals, and branded types
    - Use `Record<string, T>` for open-ended maps, not index signatures

  </Step>

  <Step title="Add the Zod schema">
    Add the corresponding Zod schema in the matching `zod-schema.*.ts` file.

    ```typescript
    // src/config/zod-schema.agent-runtime.ts
    const AgentDefaultsSchema = z
      .object({
        // ...existing fields...
        maxConcurrentTools: z.number().int().positive().optional(),
      })
      .strict();
    ```

    Rules:
    - Always use `.strict()` on object schemas — unknown keys are rejected
    - Use `.optional()` for every field (matches the type layer)
    - Add `.int()`, `.positive()`, `.nonnegative()` constraints where appropriate
    - Use `z.literal()` for fixed values and `z.enum()` for known string sets
    - Use discriminated unions (`z.discriminatedUnion()`) for variant types (see `SecretRefSchema`)
    - Mark sensitive fields with the `sensitive()` wrapper from `zod-schema.sensitive.ts`

  </Step>

  <Step title="Apply defaults">
    If the field needs a runtime default, add it to `src/config/defaults.ts` in the appropriate `apply*Defaults()` function.

    ```typescript
    export function applyAgentDefaults(cfg: DenebConfig): void {
      const defaults = cfg.agents?.defaults;
      if (defaults && defaults.maxConcurrentTools === undefined) {
        defaults.maxConcurrentTools = 4;
      }
    }
    ```

    Rules:
    - Only set defaults for fields that have meaningful non-zero defaults
    - Defaults are applied **after** validation, not before — the schema must accept `undefined`
    - Do not mutate fields the user explicitly set (always check `=== undefined`)

  </Step>

  <Step title="Update documentation">
    Add the field to `docs/gateway/configuration-reference.md` in the correct section. If the field introduces a new user-facing capability, add a task entry in `docs/gateway/configuration.md` under **Common tasks**.

    Follow [Docs Syntax Rules](/gateway/configuration-guidelines#documentation-conventions) below.

  </Step>

  <Step title="Add tests">
    Add tests in the colocated `*.test.ts` file for:
    - Schema acceptance (valid values pass)
    - Schema rejection (invalid values produce clear errors)
    - Default application (undefined becomes the expected default)
    - Round-trip (write then read preserves the value)
  </Step>

  <Step title="Run gates">
    ```bash
    pnpm check            # format + lint + type check + boundary checks
    pnpm test -- src/config/  # run config tests
    ```
  </Step>
</Steps>

## Adding a new config section

When adding an entirely new top-level section (for example `notifications`):

1. Create `src/config/types.notifications.ts` with the section type
2. Import and add it to `DenebConfig` in `src/config/types.deneb.ts`
3. Create `src/config/zod-schema.notifications.ts` with the Zod schema
4. Import and add it to `DenebSchema` in `src/config/zod-schema.ts`
5. If defaults are needed, add an `applyNotificationDefaults()` function in `defaults.ts`
6. Decide whether the section is hot-reloadable or requires a gateway restart — update the reload classification in the gateway reload logic
7. If the section contains critical operator data, add its key to `CRITICAL_KEYS` in `integrity-guard.ts`

## Schema conventions

### Strict mode

All object schemas use `.strict()`. This rejects unknown keys and prevents config drift:

```typescript
// Good
const MySchema = z.object({ enabled: z.boolean().optional() }).strict();

// Bad — allows arbitrary extra keys
const MySchema = z.object({ enabled: z.boolean().optional() });
```

### String enums

Use `z.enum()` for known string sets. Document allowed values in the type's JSDoc:

```typescript
/** DM policy for inbound messages. */
dmPolicy?: "pairing" | "allowlist" | "open" | "disabled";
```

```typescript
const DmPolicySchema = z.enum(["pairing", "allowlist", "open", "disabled"]).optional();
```

### Duration and byte-size strings

Use the existing parsers for human-readable durations and byte sizes:

```typescript
// Duration: "30m", "2h", "1d"
interval: z.string().refine(v => parseDurationMs(v) !== null, "Invalid duration").optional(),

// Byte size: "2mb", "512kb"
maxBytes: z.string().refine(v => parseByteSize(v) !== null, "Invalid byte size").optional(),
```

### Secret fields

Fields that hold credentials must use `SecretInputSchema` (accepts both raw strings and `SecretRef` objects):

```typescript
apiKey: SecretInputSchema.optional(),
```

Mark the field as sensitive:

```typescript
apiKey: sensitive(SecretInputSchema.optional()),
```

### No `anyOf` / `oneOf` in tool schemas

Per repo conventions, avoid `Type.Union` in tool input schemas. Use `z.discriminatedUnion()` with an explicit discriminator field instead.

## Type conventions

### All fields optional

Every config field must be optional. Deneb must boot with an empty config file (`{}`):

```typescript
// Good
export type GatewayConfig = {
  port?: number;
  bind?: string;
};

// Bad — required fields break empty-config boot
export type GatewayConfig = {
  port: number;
  bind?: string;
};
```

### Naming

- Use **camelCase** for all config keys
- Use descriptive names: `maxConcurrentTools` not `maxTools`
- Boolean fields: use positive names (`enabled`, `allowX`) — avoid double negatives (`disableX: false`)
- Duration fields: suffix with unit if numeric (`timeoutMs`, `intervalMinutes`) or accept duration strings (`every: "30m"`)
- Byte fields: suffix with `Bytes` if numeric or accept byte-size strings (`maxBytes: "2mb"`)

### Avoid breaking changes

Config changes must be backward compatible:

- **Adding** a new optional field is always safe
- **Removing** a field requires a legacy migration in `legacy-migrate.ts`
- **Renaming** a field requires keeping the old name as a legacy alias with migration
- **Changing** a field's type requires a migration path

## Validation conventions

### Error messages

Zod error messages should be actionable. Include the expected format or allowed values:

```typescript
// Good
z.string().regex(
  /^[a-z][a-z0-9-]*$/,
  'Must be lowercase alphanumeric with hyphens (example: "my-plugin")',
);

// Bad
z.string().regex(/^[a-z][a-z0-9-]*$/);
```

### Validation issues

The validation pipeline maps Zod errors to `ConfigValidationIssue` objects with:

- `path` — dot-notation path to the failing field
- `message` — human-readable error
- `allowedValues` — list of valid options (auto-extracted from Zod enums)

When validation fails, the gateway refuses to start. Only diagnostic commands (`deneb doctor`, `deneb status`) remain available.

### Plugin schema validation

Plugins can extend the config schema dynamically. Use `validateConfigObjectWithPlugins()` when plugin manifests are available, and `validateConfigObject()` for fast validation without plugins.

## I/O conventions

### Atomic writes

Config writes use atomic file operations (write to temp, rename). Never write directly to `deneb.json`.

### Integrity guards

The integrity guard (`integrity-guard.ts`) runs before every write and blocks:

- Removal of critical top-level keys (`gateway`, `models`, `agents`, `channels`, `secrets`, `auth`)
- Bulk key removal (keys present before but missing after)
- Size drops below 40% of previous file size

Bypass with `force: true` in `ConfigWriteOptions` or `DENEB_CONFIG_FORCE_WRITE=1`.

### Env var preservation

The I/O layer preserves `${ENV_VAR}` references in config values during write. Do not resolve env vars before writing — the merge-patch system handles this.

### Config caching

Config reads can be cached via `DENEB_CONFIG_CACHE_MS`. Always call cache-clear after writes.

## Hot reload classification

When adding config fields, decide whether changes can be applied at runtime (hot reload) or require a gateway restart:

| Hot-reloadable               | Restart required                    |
| ---------------------------- | ----------------------------------- |
| `channels.*`                 | `gateway.*` (port, bind, auth, TLS) |
| `agents.*`, `models.*`       | `discovery`                         |
| `hooks`, `cron`              | `canvasHost`                        |
| `session`, `messages`        | `plugins` (install/uninstall)       |
| `tools`, `browser`, `skills` |                                     |
| `ui`, `logging`, `bindings`  |                                     |

Update the reload classification in the gateway reload logic when adding new sections.

## `$include` support

Config supports modular composition via `$include`:

```json5
{
  $include: ["./base.json5", "./channels.json5"],
  agents: { defaults: { workspace: "~/.deneb/workspace" } },
}
```

Rules:

- Max include depth: 10 levels
- Max included file size: 2 MB
- Circular includes are detected and rejected
- Later files override earlier ones (deep merge)
- Sibling keys override included values

## Documentation conventions

When documenting config fields:

- Use `json5` (not `json`) for config code blocks — supports comments and trailing commas
- Use inline code for field paths: `agents.defaults.workspace`
- Use root-relative links: `[Configuration](/gateway/configuration)`
- Include the default value and allowed values for every field
- Add a practical example for non-trivial fields
- Keep examples minimal — show only the relevant section, not the full config

## Testing checklist

For any config change, verify:

- [ ] Type in `types.*.ts` matches Zod schema in `zod-schema.*.ts`
- [ ] Schema uses `.strict()` on all object definitions
- [ ] All fields are optional
- [ ] Defaults applied correctly in `defaults.ts`
- [ ] Validation errors produce actionable messages
- [ ] Config round-trip (write then read) preserves values
- [ ] `pnpm check` passes
- [ ] `pnpm test -- src/config/` passes
- [ ] Documentation updated in `configuration-reference.md`
- [ ] Config baseline regenerated if needed: `pnpm config:docs:gen`

---

_Related: [Configuration](/gateway/configuration) · [Configuration Reference](/gateway/configuration-reference) · [Configuration Examples](/gateway/configuration-examples)_
