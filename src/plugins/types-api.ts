import type { IncomingMessage, ServerResponse } from "node:http";
import type { Command } from "commander";
import type { AnyAgentTool } from "../agents/tools/common.js";
import type { ChannelPlugin } from "../channels/plugins/types.js";
import type { DenebConfig } from "../config/config.js";
import type { GatewayRequestHandler } from "../gateway/server-methods/types.js";
import type { InternalHookHandler } from "../hooks/internal-hooks.js";
import type { HookEntry } from "../hooks/types.js";
import type { PluginRuntime } from "./runtime/types.js";
import type {
  DenebPluginCommandDefinition,
  PluginConversationBindingResolvedEvent,
} from "./types-commands.js";
import type { PluginHookName, PluginHookHandlerMap } from "./types-hooks.js";
import type { PluginInteractiveHandlerRegistration } from "./types-interactive.js";
import type { ProviderPlugin } from "./types-provider.js";
import type {
  SpeechProviderPlugin,
  MediaUnderstandingProviderPlugin,
  ImageGenerationProviderPlugin,
} from "./types-speech-media.js";
import type { WebSearchProviderPlugin } from "./types-web-search.js";

export type PluginLogger = {
  debug?: (message: string) => void;
  info: (message: string) => void;
  warn: (message: string) => void;
  error: (message: string) => void;
};

export type PluginConfigUiHint = {
  label?: string;
  help?: string;
  tags?: string[];
  advanced?: boolean;
  sensitive?: boolean;
  placeholder?: string;
};

export type PluginKind = "memory" | "context-engine";

export type PluginConfigValidation =
  | { ok: true; value?: unknown }
  | { ok: false; errors: string[] };

export type DenebPluginConfigSchema = {
  safeParse?: (value: unknown) => {
    success: boolean;
    data?: unknown;
    error?: {
      issues?: Array<{ path: Array<string | number>; message: string }>;
    };
  };
  parse?: (value: unknown) => unknown;
  validate?: (value: unknown) => PluginConfigValidation;
  uiHints?: Record<string, PluginConfigUiHint>;
  jsonSchema?: Record<string, unknown>;
};

export type DenebPluginToolContext = {
  config?: DenebConfig;
  workspaceDir?: string;
  agentDir?: string;
  agentId?: string;
  sessionKey?: string;
  /** Ephemeral session UUID — regenerated on /new and /reset. Use for per-conversation isolation. */
  sessionId?: string;
  messageChannel?: string;
  agentAccountId?: string;
  /** Trusted sender id from inbound context (runtime-provided, not tool args). */
  requesterSenderId?: string;
  /** Whether the trusted sender is an owner. */
  senderIsOwner?: boolean;
  sandboxed?: boolean;
};

export type DenebPluginToolFactory = (
  ctx: DenebPluginToolContext,
) => AnyAgentTool | AnyAgentTool[] | null | undefined;

export type DenebPluginToolOptions = {
  name?: string;
  names?: string[];
  optional?: boolean;
};

export type DenebPluginHookOptions = {
  entry?: HookEntry;
  name?: string;
  description?: string;
  register?: boolean;
};

export type DenebPluginHttpRouteAuth = "gateway" | "plugin";
export type DenebPluginHttpRouteMatch = "exact" | "prefix";

export type DenebPluginHttpRouteHandler = (
  req: IncomingMessage,
  res: ServerResponse,
) => Promise<boolean | void> | boolean | void;

export type DenebPluginHttpRouteParams = {
  path: string;
  handler: DenebPluginHttpRouteHandler;
  auth: DenebPluginHttpRouteAuth;
  match?: DenebPluginHttpRouteMatch;
  replaceExisting?: boolean;
};

export type DenebPluginCliContext = {
  program: Command;
  config: DenebConfig;
  workspaceDir?: string;
  logger: PluginLogger;
};

export type DenebPluginCliRegistrar = (ctx: DenebPluginCliContext) => void | Promise<void>;

export type DenebPluginServiceContext = {
  config: DenebConfig;
  workspaceDir?: string;
  stateDir: string;
  logger: PluginLogger;
};

export type DenebPluginService = {
  id: string;
  start: (ctx: DenebPluginServiceContext) => void | Promise<void>;
  stop?: (ctx: DenebPluginServiceContext) => void | Promise<void>;
};

export type DenebPluginChannelRegistration = {
  plugin: ChannelPlugin;
};

export type DenebPluginDefinition = {
  id?: string;
  name?: string;
  description?: string;
  version?: string;
  kind?: PluginKind;
  configSchema?: DenebPluginConfigSchema;
  register?: (api: DenebPluginApi) => void | Promise<void>;
  activate?: (api: DenebPluginApi) => void | Promise<void>;
};

export type DenebPluginModule =
  | DenebPluginDefinition
  | ((api: DenebPluginApi) => void | Promise<void>);

export type PluginRegistrationMode = "full" | "setup-only" | "setup-runtime";

export type DenebPluginApi = {
  id: string;
  name: string;
  version?: string;
  description?: string;
  source: string;
  rootDir?: string;
  registrationMode: PluginRegistrationMode;
  config: DenebConfig;
  pluginConfig?: Record<string, unknown>;
  /**
   * In-process runtime helpers for trusted native plugins.
   *
   * This surface is broader than hooks. Prefer hooks for third-party
   * automation/integration unless you need native registry integration.
   */
  runtime: PluginRuntime;
  logger: PluginLogger;
  registerTool: (
    tool: AnyAgentTool | DenebPluginToolFactory,
    opts?: DenebPluginToolOptions,
  ) => void;
  registerHook: (
    events: string | string[],
    handler: InternalHookHandler,
    opts?: DenebPluginHookOptions,
  ) => void;
  registerHttpRoute: (params: DenebPluginHttpRouteParams) => void;
  /** Register a native messaging channel plugin (channel capability). */
  registerChannel: (registration: DenebPluginChannelRegistration | ChannelPlugin) => void;
  registerGatewayMethod: (method: string, handler: GatewayRequestHandler) => void;
  registerCli: (registrar: DenebPluginCliRegistrar, opts?: { commands?: string[] }) => void;
  registerService: (service: DenebPluginService) => void;
  /** Register a native model/provider plugin (text inference capability). */
  registerProvider: (provider: ProviderPlugin) => void;
  /** Register a speech synthesis provider (speech capability). */
  registerSpeechProvider: (provider: SpeechProviderPlugin) => void;
  /** Register a media understanding provider (media understanding capability). */
  registerMediaUnderstandingProvider: (provider: MediaUnderstandingProviderPlugin) => void;
  /** Register an image generation provider (image generation capability). */
  registerImageGenerationProvider: (provider: ImageGenerationProviderPlugin) => void;
  /** Register a web search provider (web search capability). */
  registerWebSearchProvider: (provider: WebSearchProviderPlugin) => void;
  registerInteractiveHandler: (registration: PluginInteractiveHandlerRegistration) => void;
  onConversationBindingResolved: (
    handler: (event: PluginConversationBindingResolvedEvent) => void | Promise<void>,
  ) => void;
  /**
   * Register a custom command that bypasses the LLM agent.
   * Plugin commands are processed before built-in commands and before agent invocation.
   * Use this for simple state-toggling or status commands that don't need AI reasoning.
   */
  registerCommand: (command: DenebPluginCommandDefinition) => void;
  /** Register a context engine implementation (exclusive slot — only one active at a time). */
  registerContextEngine: (
    id: string,
    factory: import("../context-engine/registry.js").ContextEngineFactory,
  ) => void;
  resolvePath: (input: string) => string;
  /** Register a lifecycle hook handler */
  on: <K extends PluginHookName>(
    hookName: K,
    handler: PluginHookHandlerMap[K],
    opts?: { priority?: number },
  ) => void;
};

export type PluginOrigin = "bundled" | "global" | "workspace" | "config";

export type PluginFormat = "deneb" | "bundle";

export type PluginBundleFormat = "codex" | "claude" | "cursor";

export type PluginDiagnostic = {
  level: "warn" | "error";
  message: string;
  pluginId?: string;
  source?: string;
};
