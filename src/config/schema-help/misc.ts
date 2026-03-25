import { describeTalkSilenceTimeoutDefaults } from "../talk-defaults.js";

export const MISC_HELP: Record<string, string> = {
  meta: "Metadata fields automatically maintained by Deneb to record write/version history for this config file. Keep these values system-managed and avoid manual edits unless debugging migration history.",
  "meta.lastTouchedVersion": "Auto-set when Deneb writes the config.",
  "meta.lastTouchedAt": "ISO timestamp of the last config write (auto-set).",
  env: "Environment import and override settings used to supply runtime variables to the gateway process. Use this section to control shell-env loading and explicit variable injection behavior.",
  "env.shellEnv":
    "Shell environment import controls for loading variables from your login shell during startup. Keep this enabled when you depend on profile-defined secrets or PATH customizations.",
  "env.shellEnv.enabled":
    "Enables loading environment variables from the user shell profile during startup initialization. Keep enabled for developer machines, or disable in locked-down service environments with explicit env management.",
  "env.shellEnv.timeoutMs":
    "Maximum time in milliseconds allowed for shell environment resolution before fallback behavior applies. Use tighter timeouts for faster startup, or increase when shell initialization is heavy.",
  "env.vars":
    "Explicit key/value environment variable overrides merged into runtime process environment for Deneb. Use this for deterministic env configuration instead of relying only on shell profile side effects.",
  wizard:
    "Setup wizard state tracking fields that record the most recent guided setup run details. Keep these fields for observability and troubleshooting of setup flows across upgrades.",
  "wizard.lastRunAt":
    "ISO timestamp for when the setup wizard most recently completed on this host. Use this to confirm setup recency during support and operational audits.",
  "wizard.lastRunVersion":
    "Deneb version recorded at the time of the most recent wizard run on this config. Use this when diagnosing behavior differences across version-to-version setup changes.",
  "wizard.lastRunCommit":
    "Source commit identifier recorded for the last wizard execution in development builds. Use this to correlate setup behavior with exact source state during debugging.",
  "wizard.lastRunCommand":
    "Command invocation recorded for the latest wizard run to preserve execution context. Use this to reproduce setup steps when verifying setup regressions.",
  "wizard.lastRunMode":
    'Wizard execution mode recorded as "local" or "remote" for the most recent setup flow. Use this to understand whether setup targeted direct local runtime or remote gateway topology.',
  diagnostics:
    "Diagnostics controls for targeted tracing, telemetry export, and cache inspection during debugging. Keep baseline diagnostics minimal in production and enable deeper signals only when investigating issues.",
  "diagnostics.otel":
    "OpenTelemetry export settings for traces, metrics, and logs emitted by gateway components. Use this when integrating with centralized observability backends and distributed tracing pipelines.",
  "diagnostics.cacheTrace":
    "Cache-trace logging settings for observing cache decisions and payload context in embedded runs. Enable this temporarily for debugging and disable afterward to reduce sensitive log footprint.",
  logging:
    "Logging behavior controls for severity, output destinations, formatting, and sensitive-data redaction. Keep levels and redaction strict enough for production while preserving useful diagnostics.",
  "logging.level":
    'Primary log level threshold for runtime logger output: "silent", "fatal", "error", "warn", "info", "debug", or "trace". Keep "info" or "warn" for production, and use debug/trace only during investigation.',
  "logging.file":
    "Optional file path for persisted log output in addition to or instead of console logging. Use a managed writable path and align retention/rotation with your operational policy.",
  "logging.consoleLevel":
    'Console-specific log threshold: "silent", "fatal", "error", "warn", "info", "debug", or "trace" for terminal output control. Use this to keep local console quieter while retaining richer file logging if needed.',
  "logging.consoleStyle":
    'Console output format style: "pretty", "compact", or "json" based on operator and ingestion needs. Use json for machine parsing pipelines and pretty/compact for human-first terminal workflows.',
  "logging.redactSensitive":
    'Sensitive redaction mode: "off" disables built-in masking, while "tools" redacts sensitive tool/config payload fields. Keep "tools" in shared logs unless you have isolated secure log sinks.',
  "logging.redactPatterns":
    "Additional custom redact regex patterns applied to log output before emission/storage. Use this to mask org-specific tokens and identifiers not covered by built-in redaction rules.",
  cli: "CLI presentation controls for local command output behavior such as banner and tagline style. Use this section to keep startup output aligned with operator preference without changing runtime behavior.",
  "cli.banner":
    "CLI startup banner controls for title/version line and tagline style behavior. Keep banner enabled for fast version/context checks, then tune tagline mode to your preferred noise level.",
  "cli.banner.taglineMode":
    'Controls tagline style in the CLI startup banner: "random" (default) picks from the rotating tagline pool, "default" always shows the neutral default tagline, and "off" hides tagline text while keeping the banner version line.',
  update:
    "Update-channel and startup-check behavior for keeping Deneb runtime versions current. Use conservative channels in production and more experimental channels only in controlled environments.",
  "update.channel": 'Update channel for git + npm installs ("stable", "beta", or "dev").',
  "update.checkOnStart": "Check for npm updates when the gateway starts (default: true).",
  "update.auto.enabled": "Enable background auto-update for package installs (default: false).",
  "update.auto.stableDelayHours":
    "Minimum delay before stable-channel auto-apply starts (default: 6).",
  "update.auto.stableJitterHours":
    "Extra stable-channel rollout spread window in hours (default: 12).",
  "update.auto.betaCheckIntervalHours": "How often beta-channel checks run in hours (default: 1).",
  "talk.provider": 'Active Talk provider id (for example "elevenlabs").',
  "talk.providers":
    "Provider-specific Talk settings keyed by provider id. During migration, prefer this over legacy talk.* keys.",
  "talk.providers.*.voiceId": "Provider default voice ID for Talk mode.",
  "talk.providers.*.voiceAliases": "Optional provider voice alias map for Talk directives.",
  "talk.providers.*.modelId": "Provider default model ID for Talk mode.",
  "talk.providers.*.outputFormat": "Provider default output format for Talk mode.",
  "talk.providers.*.apiKey": "Provider API key for Talk mode.", // pragma: allowlist secret
  "talk.voiceId":
    "Legacy ElevenLabs default voice ID for Talk mode. Prefer talk.providers.elevenlabs.voiceId.",
  "talk.voiceAliases":
    'Use this legacy ElevenLabs voice alias map (for example {"Clawd":"EXAVITQu4vr4xnSDxMaL"}) only during migration. Prefer talk.providers.elevenlabs.voiceAliases.',
  "talk.modelId":
    "Legacy ElevenLabs model ID for Talk mode (default: eleven_v3). Prefer talk.providers.elevenlabs.modelId.",
  "talk.outputFormat":
    "Use this legacy ElevenLabs output format for Talk mode (for example pcm_44100 or mp3_44100_128) only during migration. Prefer talk.providers.elevenlabs.outputFormat.",
  "talk.apiKey":
    "Use this legacy ElevenLabs API key for Talk mode only during migration, and keep secrets in env-backed storage. Prefer talk.providers.elevenlabs.apiKey (fallback: ELEVENLABS_API_KEY).",
  "talk.interruptOnSpeech":
    "If true (default), stop assistant speech when the user starts speaking in Talk mode. Keep enabled for conversational turn-taking.",
  "talk.silenceTimeoutMs": `Milliseconds of user silence before Talk mode finalizes and sends the current transcript. Leave unset to keep the platform default pause window (${describeTalkSilenceTimeoutDefaults()}).`,
  acp: "ACP runtime controls for enabling dispatch, selecting backends, constraining allowed agent targets, and tuning streamed turn projection behavior.",
  "acp.enabled":
    "Global ACP feature gate. Keep disabled unless ACP runtime + policy are configured.",
  "acp.dispatch.enabled":
    "Independent dispatch gate for ACP session turns (default: true). Set false to keep ACP commands available while blocking ACP turn execution.",
  "acp.backend":
    "Default ACP runtime backend id (for example: acpx). Must match a registered ACP runtime plugin backend.",
  "acp.defaultAgent":
    "Fallback ACP target agent id used when ACP spawns do not specify an explicit target.",
  "acp.allowedAgents":
    "Allowlist of ACP target agent ids permitted for ACP runtime sessions. Empty means no additional allowlist restriction.",
  "acp.maxConcurrentSessions":
    "Maximum concurrently active ACP sessions across this gateway process.",
  "acp.stream":
    "ACP streaming projection controls for chunk sizing, metadata visibility, and deduped delivery behavior.",
  "acp.stream.coalesceIdleMs":
    "Coalescer idle flush window in milliseconds for ACP streamed text before block replies are emitted.",
  "acp.stream.maxChunkChars":
    "Maximum chunk size for ACP streamed block projection before splitting into multiple block replies.",
  "acp.stream.repeatSuppression":
    "When true (default), suppress repeated ACP status/tool projection lines in a turn while keeping raw ACP events unchanged.",
  "acp.stream.deliveryMode":
    "ACP delivery style: live streams projected output incrementally, final_only buffers all projected ACP output until terminal turn events.",
  "acp.stream.hiddenBoundarySeparator":
    "Separator inserted before next visible assistant text when hidden ACP tool lifecycle events occurred (none|space|newline|paragraph). Default: paragraph.",
  "acp.stream.maxOutputChars":
    "Maximum assistant output characters projected per ACP turn before truncation notice is emitted.",
  "acp.stream.maxSessionUpdateChars":
    "Maximum characters for projected ACP session/update lines (tool/status updates).",
  "acp.stream.tagVisibility":
    "Per-sessionUpdate visibility overrides for ACP projection (for example usage_update, available_commands_update).",
  "acp.runtime.ttlMinutes":
    "Idle runtime TTL in minutes for ACP session workers before eligible cleanup.",
  "acp.runtime.installCommand":
    "Optional operator install/setup command shown by `/acp install` and `/acp doctor` when ACP backend wiring is missing.",
  "discovery.mdns.mode":
    'mDNS broadcast mode ("minimal" default, "full" includes cliPath/sshPort, "off" disables mDNS).',
  discovery:
    "Service discovery settings for local mDNS advertisement and optional wide-area presence signaling. Keep discovery scoped to expected networks to avoid leaking service metadata.",
  "discovery.wideArea":
    "Wide-area discovery configuration group for exposing discovery signals beyond local-link scopes. Enable only in deployments that intentionally aggregate gateway presence across sites.",
  "discovery.wideArea.enabled":
    "Enables wide-area discovery signaling when your environment needs non-local gateway discovery. Keep disabled unless cross-network discovery is operationally required.",
  "discovery.wideArea.domain":
    "Optional unicast DNS-SD domain for wide-area discovery, such as deneb.internal. Use this when you intentionally publish gateway discovery beyond local mDNS scopes.",
  "discovery.mdns":
    "mDNS discovery configuration group for local network advertisement and discovery behavior tuning. Keep minimal mode for routine LAN discovery unless extra metadata is required.",
  web: "Web channel runtime settings for heartbeat and reconnect behavior when operating web-based chat surfaces. Use reconnect values tuned to your network reliability profile and expected uptime needs.",
  "web.enabled":
    "Enables the web channel runtime and related websocket lifecycle behavior. Keep disabled when web chat is unused to reduce active connection management overhead.",
  "web.heartbeatSeconds":
    "Heartbeat interval in seconds for web channel connectivity and liveness maintenance. Use shorter intervals for faster detection, or longer intervals to reduce keepalive chatter.",
  "web.reconnect":
    "Reconnect backoff policy for web channel reconnect attempts after transport failure. Keep bounded retries and jitter tuned to avoid thundering-herd reconnect behavior.",
  "web.reconnect.initialMs":
    "Initial reconnect delay in milliseconds before the first retry after disconnection. Use modest delays to recover quickly without immediate retry storms.",
  "web.reconnect.maxMs":
    "Maximum reconnect backoff cap in milliseconds to bound retry delay growth over repeated failures. Use a reasonable cap so recovery remains timely after prolonged outages.",
  "web.reconnect.factor":
    "Exponential backoff multiplier used between reconnect attempts in web channel retry loops. Keep factor above 1 and tune with jitter for stable large-fleet reconnect behavior.",
  "web.reconnect.jitter":
    "Randomization factor (0-1) applied to reconnect delays to desynchronize clients after outage events. Keep non-zero jitter in multi-client deployments to reduce synchronized spikes.",
  "web.reconnect.maxAttempts":
    "Maximum reconnect attempts before giving up for the current failure sequence (0 means no retries). Use finite caps for controlled failure handling in automation-sensitive environments.",
  talk: "Talk-mode voice synthesis settings for voice identity, model selection, output format, and interruption behavior. Use this section to tune human-facing voice UX while controlling latency and cost.",
  nodeHost:
    "Node host controls for features exposed from this gateway node to other nodes or clients. Keep defaults unless you intentionally proxy local capabilities across your node network.",
  "nodeHost.browserProxy":
    "Groups browser-proxy settings for exposing local browser control through node routing. Enable only when remote node workflows need your local browser profiles.",
  "nodeHost.browserProxy.enabled":
    "Expose the local browser control server through node proxy routing so remote clients can use this host's browser capabilities. Keep disabled unless remote automation explicitly depends on it.",
  "nodeHost.browserProxy.allowProfiles":
    "Optional allowlist of browser profile names exposed through node proxy routing. Leave empty to expose all configured profiles, or use a tight list to enforce least-privilege profile access.",
  media:
    "Top-level media behavior shared across providers and tools that handle inbound files. Keep defaults unless you need stable filenames for external processing pipelines or longer-lived inbound media retention.",
  "media.preserveFilenames":
    "When enabled, uploaded media keeps its original filename instead of a generated temp-safe name. Turn this on when downstream automations depend on stable names, and leave off to reduce accidental filename leakage.",
  "media.ttlHours":
    "Optional retention window in hours for persisted inbound media cleanup across the full media tree. Leave unset to preserve legacy behavior, or set values like 24 (1 day) or 168 (7 days) when you want automatic cleanup.",
  audio:
    "Global audio ingestion settings used before higher-level tools process speech or media content. Configure this when you need deterministic transcription behavior for voice notes and clips.",
  "audio.transcription":
    "Command-based transcription settings for converting audio files into text before agent handling. Keep a simple, deterministic command path here so failures are easy to diagnose in logs.",
  "audio.transcription.command":
    'Executable + args used to transcribe audio (first token must be a safe binary/path), for example `["whisper-cli", "--model", "small", "{input}"]`. Prefer a pinned command so runtime environments behave consistently.',
  "audio.transcription.timeoutSeconds":
    "Maximum time allowed for the transcription command to finish before it is aborted. Increase this for longer recordings, and keep it tight in latency-sensitive deployments.",
  bindings:
    "Top-level binding rules for routing and persistent ACP conversation ownership. Use type=route for normal routing and type=acp for persistent ACP harness bindings.",
  "bindings[].type":
    'Binding kind. Use "route" (or omit for legacy route entries) for normal routing, and "acp" for persistent ACP conversation bindings.',
  "bindings[].agentId":
    "Target agent ID that receives traffic when the corresponding binding match rule is satisfied. Use valid configured agent IDs only so routing does not fail at runtime.",
  "bindings[].match":
    "Match rule object for deciding when a binding applies, including channel and optional account/peer constraints. Keep rules narrow to avoid accidental agent takeover across contexts.",
  "bindings[].match.channel":
    "Channel/provider identifier this binding applies to, such as `telegram` or a plugin channel ID. Use the configured channel key exactly so binding evaluation works reliably.",
  "bindings[].match.accountId":
    "Optional account selector for multi-account channel setups so the binding applies only to one identity. Use this when account scoping is required for the route and leave unset otherwise.",
  "bindings[].match.peer":
    "Optional peer matcher for specific conversations including peer kind and peer id. Use this when only one direct/group/channel target should be pinned to an agent.",
  "bindings[].match.peer.kind":
    'Peer conversation type: "direct", "group", "channel", or legacy "dm" (deprecated alias for direct). Prefer "direct" for new configs and keep kind aligned with channel semantics.',
  "bindings[].match.peer.id":
    "Conversation identifier used with peer matching, such as a chat ID, channel ID, or group ID from the provider. Keep this exact to avoid silent non-matches.",
  "bindings[].match.guildId":
    "Optional Discord-style guild/server ID constraint for binding evaluation in multi-server deployments. Use this when the same peer identifiers can appear across different guilds.",
  "bindings[].match.teamId":
    "Optional team/workspace ID constraint used by providers that scope chats under teams. Add this when you need bindings isolated to one workspace context.",
  "bindings[].match.roles":
    "Optional role-based filter list used by providers that attach roles to chat context. Use this to route privileged or operational role traffic to specialized agents.",
  "bindings[].acp":
    "Optional per-binding ACP overrides for bindings[].type=acp. This layer overrides agents.list[].runtime.acp defaults for the matched conversation.",
  "bindings[].acp.mode": "ACP session mode override for this binding (persistent or oneshot).",
  "bindings[].acp.label":
    "Human-friendly label for ACP status/diagnostics in this bound conversation.",
  "bindings[].acp.cwd": "Working directory override for ACP sessions created from this binding.",
  "bindings[].acp.backend":
    "ACP backend override for this binding (falls back to agent runtime ACP backend, then global acp.backend).",
  broadcast:
    "Broadcast routing map for sending the same outbound message to multiple peer IDs per source conversation. Keep this minimal and audited because one source can fan out to many destinations.",
  "broadcast.strategy":
    'Delivery order for broadcast fan-out: "parallel" sends to all targets concurrently, while "sequential" sends one-by-one. Use "parallel" for speed and "sequential" for stricter ordering/backpressure control.',
  "broadcast.*":
    "Per-source broadcast destination list where each key is a source peer ID and the value is an array of destination peer IDs. Keep lists intentional to avoid accidental message amplification.",
  "diagnostics.flags":
    'Enable targeted diagnostics logs by flag (e.g. ["telegram.http"]). Supports wildcards like "telegram.*" or "*".',
  "diagnostics.enabled":
    "Master toggle for diagnostics instrumentation output in logs and telemetry wiring paths. Keep enabled for normal observability, and disable only in tightly constrained environments.",
  "diagnostics.stuckSessionWarnMs":
    "Age threshold in milliseconds for emitting stuck-session warnings while a session remains in processing state. Increase for long multi-tool turns to reduce false positives; decrease for faster hang detection.",
  "diagnostics.otel.enabled":
    "Enables OpenTelemetry export pipeline for traces, metrics, and logs based on configured endpoint/protocol settings. Keep disabled unless your collector endpoint and auth are fully configured.",
  "diagnostics.otel.endpoint":
    "Collector endpoint URL used for OpenTelemetry export transport, including scheme and port. Use a reachable, trusted collector endpoint and monitor ingestion errors after rollout.",
  "diagnostics.otel.protocol":
    'OTel transport protocol for telemetry export: "http/protobuf" or "grpc" depending on collector support. Use the protocol your observability backend expects to avoid dropped telemetry payloads.',
  "diagnostics.otel.headers":
    "Additional HTTP/gRPC metadata headers sent with OpenTelemetry export requests, often used for tenant auth or routing. Keep secrets in env-backed values and avoid unnecessary header sprawl.",
  "diagnostics.otel.serviceName":
    "Service name reported in telemetry resource attributes to identify this gateway instance in observability backends. Use stable names so dashboards and alerts remain consistent over deployments.",
  "diagnostics.otel.traces":
    "Enable trace signal export to the configured OpenTelemetry collector endpoint. Keep enabled when latency/debug tracing is needed, and disable if you only want metrics/logs.",
  "diagnostics.otel.metrics":
    "Enable metrics signal export to the configured OpenTelemetry collector endpoint. Keep enabled for runtime health dashboards, and disable only if metric volume must be minimized.",
  "diagnostics.otel.logs":
    "Enable log signal export through OpenTelemetry in addition to local logging sinks. Use this when centralized log correlation is required across services and agents.",
  "diagnostics.otel.sampleRate":
    "Trace sampling rate (0-1) controlling how much trace traffic is exported to observability backends. Lower rates reduce overhead/cost, while higher rates improve debugging fidelity.",
  "diagnostics.otel.flushIntervalMs":
    "Interval in milliseconds for periodic telemetry flush from buffers to the collector. Increase to reduce export chatter, or lower for faster visibility during active incident response.",
  "diagnostics.cacheTrace.enabled":
    "Log cache trace snapshots for embedded agent runs (default: false).",
  "diagnostics.cacheTrace.filePath":
    "JSONL output path for cache trace logs (default: $DENEB_STATE_DIR/logs/cache-trace.jsonl).",
  "diagnostics.cacheTrace.includeMessages":
    "Include full message payloads in trace output (default: true).",
  "diagnostics.cacheTrace.includePrompt": "Include prompt text in trace output (default: true).",
  "diagnostics.cacheTrace.includeSystem": "Include system prompt in trace output (default: true).",
  approvals:
    "Approval routing controls for forwarding exec approval requests to chat destinations outside the originating session. Keep this disabled unless operators need explicit out-of-band approval visibility.",
  "approvals.exec":
    "Groups exec-approval forwarding behavior including enablement, routing mode, filters, and explicit targets. Configure here when approval prompts must reach operational channels instead of only the origin thread.",
  "approvals.exec.enabled":
    "Enables forwarding of exec approval requests to configured delivery destinations (default: false). Keep disabled in low-risk setups and enable only when human approval responders need channel-visible prompts.",
  "approvals.exec.mode":
    'Controls where approval prompts are sent: "session" uses origin chat, "targets" uses configured targets, and "both" sends to both paths. Use "session" as baseline and expand only when operational workflow requires redundancy.',
  "approvals.exec.agentFilter":
    'Optional allowlist of agent IDs eligible for forwarded approvals, for example `["primary", "ops-agent"]`. Use this to limit forwarding blast radius and avoid notifying channels for unrelated agents.',
  "approvals.exec.sessionFilter":
    'Optional session-key filters matched as substring or regex-style patterns, for example `["discord:", "^agent:ops:"]`. Use narrow patterns so only intended approval contexts are forwarded to shared destinations.',
  "approvals.exec.targets":
    "Explicit delivery targets used when forwarding mode includes targets, each with channel and destination details. Keep target lists least-privilege and validate each destination before enabling broad forwarding.",
  "approvals.exec.targets[].channel":
    "Channel/provider ID used for forwarded approval delivery, such as telegram or a plugin channel id. Use valid channel IDs only so approvals do not silently fail due to unknown routes.",
  "approvals.exec.targets[].to":
    "Destination identifier inside the target channel (channel ID, user ID, or thread root depending on provider). Verify semantics per provider because destination format differs across channel integrations.",
  "approvals.exec.targets[].accountId":
    "Optional account selector for multi-account channel setups when approvals must route through a specific account context. Use this only when the target channel has multiple configured identities.",
  "approvals.exec.targets[].threadId":
    "Optional thread/topic target for channels that support threaded delivery of forwarded approvals. Use this to keep approval traffic contained in operational threads instead of main channels.",
  ui: "UI presentation settings for accenting and assistant identity shown in control surfaces. Use this for branding and readability customization without changing runtime behavior.",
  "ui.seamColor":
    "Primary accent color used by UI surfaces for emphasis, badges, and visual identity cues. Use high-contrast values that remain readable across light/dark themes.",
  "ui.assistant":
    "Assistant display identity settings for name and avatar shown in UI surfaces. Keep these values aligned with your operator-facing persona and support expectations.",
  "ui.assistant.name":
    "Display name shown for the assistant in UI views, chat chrome, and status contexts. Keep this stable so operators can reliably identify which assistant persona is active.",
  "ui.assistant.avatar":
    "Assistant avatar image source used in UI surfaces (URL, path, or data URI depending on runtime support). Use trusted assets and consistent branding dimensions for clean rendering.",
  commands:
    "Controls chat command surfaces, owner gating, and elevated command access behavior across providers. Keep defaults unless you need stricter operator controls or broader command availability.",
  "commands.native":
    "Registers native slash/menu commands with channels that support command registration (Discord, Slack, Telegram). Keep enabled for discoverability unless you intentionally run text-only command workflows.",
  "commands.nativeSkills":
    "Registers native skill commands so users can invoke skills directly from provider command menus where supported. Keep aligned with your skill policy so exposed commands match what operators expect.",
  "commands.text":
    "Enables text-command parsing in chat input in addition to native command surfaces where available. Keep this enabled for compatibility across channels that do not support native command registration.",
  "commands.bash":
    "Allow bash chat command (`!`; `/bash` alias) to run host shell commands (default: false; requires tools.elevated).",
  "commands.bashForegroundMs":
    "How long bash waits before backgrounding (default: 2000; 0 backgrounds immediately).",
  "commands.config": "Allow /config chat command to read/write config on disk (default: false).",
  "commands.mcp":
    "Allow /mcp chat command to manage Deneb MCP server config under mcp.servers (default: false).",
  "commands.plugins":
    "Allow /plugins chat command to list discovered plugins and toggle plugin enablement in config (default: false).",
  "commands.debug": "Allow /debug chat command for runtime-only overrides (default: false).",
  "commands.restart": "Allow /restart and gateway restart tool actions (default: true).",
  "commands.useAccessGroups": "Enforce access-group allowlists/policies for commands.",
  "commands.ownerAllowFrom":
    "Explicit owner allowlist for owner-only tools/commands. Use channel-native IDs (optionally prefixed like \"whatsapp:+15551234567\"). '*' is ignored.",
  "commands.ownerDisplay":
    "Controls how owner IDs are rendered in the system prompt. Allowed values: raw, hash. Default: raw.",
  "commands.ownerDisplaySecret":
    "Optional secret used to HMAC hash owner IDs when ownerDisplay=hash. Prefer env substitution.",
  "commands.allowFrom":
    "Defines elevated command allow rules by channel and sender for owner-level command surfaces. Use narrow provider-specific identities so privileged commands are not exposed to broad chat audiences.",
  mcp: "Global MCP server definitions managed by Deneb. Embedded Pi and other runtime adapters can consume these servers without storing them inside Pi-owned project settings.",
  "mcp.servers":
    "Named MCP server definitions. Deneb stores them in its own config and runtime adapters decide which transports are supported at execution time.",
};
