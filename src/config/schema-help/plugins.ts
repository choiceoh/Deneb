export const PLUGINS_HELP: Record<string, string> = {
  plugins:
    "Plugin system controls for enabling extensions, constraining load scope, configuring entries, and tracking installs. Keep plugin policy explicit and least-privilege in production environments.",
  "plugins.enabled":
    "Enable or disable plugin/extension loading globally during startup and config reload (default: true). Keep enabled only when extension capabilities are required by your deployment.",
  "plugins.allow":
    "Optional allowlist of plugin IDs; when set, only listed plugins are eligible to load. Use this to enforce approved extension inventories in controlled environments.",
  "plugins.deny":
    "Optional denylist of plugin IDs that are blocked even if allowlists or paths include them. Use deny rules for emergency rollback and hard blocks on risky plugins.",
  "plugins.load":
    "Plugin loader configuration group for specifying filesystem paths where plugins are discovered. Keep load paths explicit and reviewed to avoid accidental untrusted extension loading.",
  "plugins.load.paths":
    "Additional plugin files or directories scanned by the loader beyond built-in defaults. Use dedicated extension directories and avoid broad paths with unrelated executable content.",
  "plugins.slots":
    "Selects which plugins own exclusive runtime slots such as memory so only one plugin provides that capability. Use explicit slot ownership to avoid overlapping providers with conflicting behavior.",
  "plugins.slots.memory":
    'Select the active memory plugin by id, or "none" to disable memory plugins.',
  "plugins.slots.contextEngine":
    "Selects the active context engine plugin by id so one plugin provides context orchestration behavior.",
  "plugins.entries":
    "Per-plugin settings keyed by plugin ID including enablement and plugin-specific runtime configuration payloads. Use this for scoped plugin tuning without changing global loader policy.",
  "plugins.entries.*.enabled":
    "Per-plugin enablement override for a specific entry, applied on top of global plugin policy (restart required). Use this to stage plugin rollout gradually across environments.",
  "plugins.entries.*.hooks":
    "Per-plugin typed hook policy controls for core-enforced safety gates. Use this to constrain high-impact hook categories without disabling the entire plugin.",
  "plugins.entries.*.hooks.allowPromptInjection":
    "Controls whether this plugin may mutate prompts through typed hooks. Set false to block `before_prompt_build` and ignore prompt-mutating fields from legacy `before_agent_start`, while preserving legacy `modelOverride` and `providerOverride` behavior.",
  "plugins.entries.*.subagent":
    "Per-plugin subagent runtime controls for model override trust and allowlists. Keep this unset unless a plugin must explicitly steer subagent model selection.",
  "plugins.entries.*.subagent.allowModelOverride":
    "Explicitly allows this plugin to request provider/model overrides in background subagent runs. Keep false unless the plugin is trusted to steer model selection.",
  "plugins.entries.*.subagent.allowedModels":
    'Allowed override targets for trusted plugin subagent runs as canonical "provider/model" refs. Use "*" only when you intentionally allow any model.',
  "plugins.entries.*.apiKey":
    "Optional API key field consumed by plugins that accept direct key configuration in entry settings. Use secret/env substitution and avoid committing real credentials into config files.",
  "plugins.entries.*.env":
    "Per-plugin environment variable map injected for that plugin runtime context only. Use this to scope provider credentials to one plugin instead of sharing global process environment.",
  "plugins.entries.*.config":
    "Plugin-defined configuration payload interpreted by that plugin's own schema and validation rules. Use only documented fields from the plugin to prevent ignored or invalid settings.",
  "plugins.installs":
    "CLI-managed install metadata (used by `deneb plugins update` to locate install sources).",
  "plugins.installs.*.source": 'Install source ("npm", "archive", or "path").',
  "plugins.installs.*.spec": "Original npm spec used for install (if source is npm).",
  "plugins.installs.*.sourcePath": "Original archive/path used for install (if any).",
  "plugins.installs.*.installPath":
    "Resolved install directory (usually ~/.deneb/extensions/<id>).",
  "plugins.installs.*.version": "Version recorded at install time (if available).",
  "plugins.installs.*.resolvedName": "Resolved npm package name from the fetched artifact.",
  "plugins.installs.*.resolvedVersion":
    "Resolved npm package version from the fetched artifact (useful for non-pinned specs).",
  "plugins.installs.*.resolvedSpec":
    "Resolved exact npm spec (<name>@<version>) from the fetched artifact.",
  "plugins.installs.*.integrity":
    "Resolved npm dist integrity hash for the fetched artifact (if reported by npm).",
  "plugins.installs.*.shasum":
    "Resolved npm dist shasum for the fetched artifact (if reported by npm).",
  "plugins.installs.*.resolvedAt":
    "ISO timestamp when npm package metadata was last resolved for this install record.",
  "plugins.installs.*.installedAt": "ISO timestamp of last install/update.",
  "plugins.installs.*.marketplaceName":
    "Marketplace display name recorded for marketplace-backed plugin installs (if available).",
  "plugins.installs.*.marketplaceSource":
    "Original marketplace source used to resolve the install (for example a repo path or Git URL).",
  "plugins.installs.*.marketplacePlugin":
    "Plugin entry name inside the source marketplace, used for later updates.",
};
