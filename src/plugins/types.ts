/**
 * Barrel re-export for plugin types.
 *
 * All type definitions have been split into domain-focused sub-files.
 * This file re-exports everything so existing `import { X } from "./types.js"`
 * imports continue to work unchanged.
 */

export type { PluginRuntime } from "./runtime/types.js";
export type { AnyAgentTool } from "../agents/tools/common.js";

// Provider types (auth, catalog, runtime, wizard, plugin)
export * from "./types-provider.js";

// Web search provider types
export * from "./types-web-search.js";

// Media understanding and image generation provider types
export * from "./types-media.js";

// Plugin commands and conversation binding types
export * from "./types-commands.js";

// Interactive handler types (Telegram, Discord, Slack)
export * from "./types-interactive.js";

// Plugin definition, API, HTTP routes, CLI, services, and shared small types
export * from "./types-api.js";

// Plugin hooks (lifecycle events, handler map, registration)
export * from "./types-hooks.js";
