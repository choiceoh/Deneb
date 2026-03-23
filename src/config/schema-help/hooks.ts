export const HOOKS_HELP: Record<string, string> = {
  hooks:
    "Inbound webhook automation surface for mapping external events into wake or agent actions in Deneb. Keep this locked down with explicit token/session/agent controls before exposing it beyond trusted networks.",
  "hooks.enabled":
    "Enables the hooks endpoint and mapping execution pipeline for inbound webhook requests. Keep disabled unless you are actively routing external events into the gateway.",
  "hooks.path":
    "HTTP path used by the hooks endpoint (for example `/hooks`) on the gateway control server. Use a non-guessable path and combine it with token validation for defense in depth.",
  "hooks.token":
    "Shared bearer token checked by hooks ingress for request authentication before mappings run. Treat holders as full-trust callers for the hook ingress surface, not as a separate non-owner role. Use environment substitution and rotate regularly when webhook endpoints are internet-accessible.",
  "hooks.defaultSessionKey":
    "Fallback session key used for hook deliveries when a request does not provide one through allowed channels. Use a stable but scoped key to avoid mixing unrelated automation conversations.",
  "hooks.allowRequestSessionKey":
    "Allows callers to supply a session key in hook requests when true, enabling caller-controlled routing. Keep false unless trusted integrators explicitly need custom session threading.",
  "hooks.allowedSessionKeyPrefixes":
    "Allowlist of accepted session-key prefixes for inbound hook requests when caller-provided keys are enabled. Use narrow prefixes to prevent arbitrary session-key injection.",
  "hooks.allowedAgentIds":
    "Allowlist of agent IDs that hook mappings are allowed to target when selecting execution agents. Use this to constrain automation events to dedicated service agents and reduce blast radius if a hook token is exposed.",
  "hooks.maxBodyBytes":
    "Maximum accepted webhook payload size in bytes before the request is rejected. Keep this bounded to reduce abuse risk and protect memory usage under bursty integrations.",
  "hooks.presets":
    "Named hook preset bundles applied at load time to seed standard mappings and behavior defaults. Keep preset usage explicit so operators can audit which automations are active.",
  "hooks.transformsDir":
    "Base directory for hook transform modules referenced by mapping transform.module paths. Use a controlled repo directory so dynamic imports remain reviewable and predictable.",
  "hooks.mappings":
    "Ordered mapping rules that match inbound hook requests and choose wake or agent actions with optional delivery routing. Use specific mappings first to avoid broad pattern rules capturing everything.",
  "hooks.mappings[].id":
    "Optional stable identifier for a hook mapping entry used for auditing, troubleshooting, and targeted updates. Use unique IDs so logs and config diffs can reference mappings unambiguously.",
  "hooks.mappings[].match":
    "Grouping object for mapping match predicates such as path and source before action routing is applied. Keep match criteria specific so unrelated webhook traffic does not trigger automations.",
  "hooks.mappings[].match.path":
    "Path match condition for a hook mapping, usually compared against the inbound request path. Use this to split automation behavior by webhook endpoint path families.",
  "hooks.mappings[].match.source":
    "Source match condition for a hook mapping, typically set by trusted upstream metadata or adapter logic. Use stable source identifiers so routing remains deterministic across retries.",
  "hooks.mappings[].action":
    'Mapping action type: "wake" triggers agent wake flow, while "agent" sends directly to agent handling. Use "agent" for immediate execution and "wake" when heartbeat-driven processing is preferred.',
  "hooks.mappings[].wakeMode":
    'Wake scheduling mode: "now" wakes immediately, while "next-heartbeat" defers until the next heartbeat cycle. Use deferred mode for lower-priority automations that can tolerate slight delay.',
  "hooks.mappings[].name":
    "Human-readable mapping display name used in diagnostics and operator-facing config UIs. Keep names concise and descriptive so routing intent is obvious during incident review.",
  "hooks.mappings[].agentId":
    "Target agent ID for mapping execution when action routing should not use defaults. Use dedicated automation agents to isolate webhook behavior from interactive operator sessions.",
  "hooks.mappings[].sessionKey":
    "Explicit session key override for mapping-delivered messages to control thread continuity. Use stable scoped keys so repeated events correlate without leaking into unrelated conversations.",
  "hooks.mappings[].messageTemplate":
    "Template for synthesizing structured mapping input into the final message content sent to the target action path. Keep templates deterministic so downstream parsing and behavior remain stable.",
  "hooks.mappings[].textTemplate":
    "Text-only fallback template used when rich payload rendering is not desired or not supported. Use this to provide a concise, consistent summary string for chat delivery surfaces.",
  "hooks.mappings[].deliver":
    "Controls whether mapping execution results are delivered back to a channel destination versus being processed silently. Disable delivery for background automations that should not post user-facing output.",
  "hooks.mappings[].allowUnsafeExternalContent":
    "When true, mapping content may include less-sanitized external payload data in generated messages. Keep false by default and enable only for trusted sources with reviewed transform logic.",
  "hooks.mappings[].channel":
    'Delivery channel override for mapping outputs (for example "last", "telegram", or a plugin channel id). Keep channel overrides explicit to avoid accidental cross-channel sends.',
  "hooks.mappings[].to":
    "Destination identifier inside the selected channel when mapping replies should route to a fixed target. Verify provider-specific destination formats before enabling production mappings.",
  "hooks.mappings[].model":
    "Optional model override for mapping-triggered runs when automation should use a different model than agent defaults. Use this sparingly so behavior remains predictable across mapping executions.",
  "hooks.mappings[].thinking":
    "Optional thinking-effort override for mapping-triggered runs to tune latency versus reasoning depth. Keep low or minimal for high-volume hooks unless deeper reasoning is clearly required.",
  "hooks.mappings[].timeoutSeconds":
    "Maximum runtime allowed for mapping action execution before timeout handling applies. Use tighter limits for high-volume webhook sources to prevent queue pileups.",
  "hooks.mappings[].transform":
    "Transform configuration block defining module/export preprocessing before mapping action handling. Use transforms only from reviewed code paths and keep behavior deterministic for repeatable automation.",
  "hooks.mappings[].transform.module":
    "Relative transform module path loaded from hooks.transformsDir to rewrite incoming payloads before delivery. Keep modules local, reviewed, and free of path traversal patterns.",
  "hooks.mappings[].transform.export":
    "Named export to invoke from the transform module; defaults to module default export when omitted. Set this when one file hosts multiple transform handlers.",
  "hooks.gmail":
    "Gmail push integration settings used for Pub/Sub notifications and optional local callback serving. Keep this scoped to dedicated Gmail automation accounts where possible.",
  "hooks.gmail.account":
    "Google account identifier used for Gmail watch/subscription operations in this hook integration. Use a dedicated automation mailbox account to isolate operational permissions.",
  "hooks.gmail.label":
    "Optional Gmail label filter limiting which labeled messages trigger hook events. Keep filters narrow to avoid flooding automations with unrelated inbox traffic.",
  "hooks.gmail.topic":
    "Google Pub/Sub topic name used by Gmail watch to publish change notifications for this account. Ensure the topic IAM grants Gmail publish access before enabling watches.",
  "hooks.gmail.subscription":
    "Pub/Sub subscription consumed by the gateway to receive Gmail change notifications from the configured topic. Keep subscription ownership clear so multiple consumers do not race unexpectedly.",
  "hooks.gmail.hookUrl":
    "Public callback URL Gmail or intermediaries invoke to deliver notifications into this hook pipeline. Keep this URL protected with token validation and restricted network exposure.",
  "hooks.gmail.includeBody":
    "When true, fetch and include email body content for downstream mapping/agent processing. Keep false unless body text is required, because this increases payload size and sensitivity.",
  "hooks.gmail.allowUnsafeExternalContent":
    "Allows less-sanitized external Gmail content to pass into processing when enabled. Keep disabled for safer defaults, and enable only for trusted mail streams with controlled transforms.",
  "hooks.gmail.serve":
    "Local callback server settings block for directly receiving Gmail notifications without a separate ingress layer. Enable only when this process should terminate webhook traffic itself.",
  "hooks.gmail.pushToken":
    "Shared secret token required on Gmail push hook callbacks before processing notifications. Use env substitution and rotate if callback endpoints are exposed externally.",
  "hooks.gmail.maxBytes":
    "Maximum Gmail payload bytes processed per event when includeBody is enabled. Keep conservative limits to reduce oversized message processing cost and risk.",
  "hooks.gmail.renewEveryMinutes":
    "Renewal cadence in minutes for Gmail watch subscriptions to prevent expiration. Set below provider expiration windows and monitor renew failures in logs.",
  "hooks.gmail.serve.bind":
    "Bind address for the local Gmail callback HTTP server used when serving hooks directly. Keep loopback-only unless external ingress is intentionally required.",
  "hooks.gmail.serve.port":
    "Port for the local Gmail callback HTTP server when serve mode is enabled. Use a dedicated port to avoid collisions with gateway/control interfaces.",
  "hooks.gmail.serve.path":
    "HTTP path on the local Gmail callback server where push notifications are accepted. Keep this consistent with subscription configuration to avoid dropped events.",
  "hooks.gmail.tailscale.mode":
    'Tailscale exposure mode for Gmail callbacks: "off", "serve", or "funnel". Use "serve" for private tailnet delivery and "funnel" only when public internet ingress is required.',
  "hooks.gmail.tailscale":
    "Tailscale exposure configuration block for publishing Gmail callbacks through Serve/Funnel routes. Use private tailnet modes before enabling any public ingress path.",
  "hooks.gmail.tailscale.path":
    "Path published by Tailscale Serve/Funnel for Gmail callback forwarding when enabled. Keep it aligned with Gmail webhook config so requests reach the expected handler.",
  "hooks.gmail.tailscale.target":
    "Local service target forwarded by Tailscale Serve/Funnel (for example http://127.0.0.1:8787). Use explicit loopback targets to avoid ambiguous routing.",
  "hooks.gmail.model":
    "Optional model override for Gmail-triggered runs when mailbox automations should use dedicated model behavior. Keep unset to inherit agent defaults unless mailbox tasks need specialization.",
  "hooks.gmail.thinking":
    'Thinking effort override for Gmail-driven agent runs: "off", "minimal", "low", "medium", or "high". Keep modest defaults for routine inbox automations to control cost and latency.',
  "hooks.internal":
    "Internal hook runtime settings for bundled/custom event handlers loaded from module paths. Use this for trusted in-process automations and keep handler loading tightly scoped.",
  "hooks.internal.enabled":
    "Enables processing for internal hook handlers and configured entries in the internal hook runtime. Keep disabled unless internal hook handlers are intentionally configured.",
  "hooks.internal.handlers":
    "List of internal event handlers mapping event names to modules and optional exports. Keep handler definitions explicit so event-to-code routing is auditable.",
  "hooks.internal.handlers[].event":
    "Internal event name that triggers this handler module when emitted by the runtime. Use stable event naming conventions to avoid accidental overlap across handlers.",
  "hooks.internal.handlers[].module":
    "Safe relative module path for the internal hook handler implementation loaded at runtime. Keep module files in reviewed directories and avoid dynamic path composition.",
  "hooks.internal.handlers[].export":
    "Optional named export for the internal hook handler function when module default export is not used. Set this when one module ships multiple handler entrypoints.",
  "hooks.internal.entries":
    "Configured internal hook entry records used to register concrete runtime handlers and metadata. Keep entries explicit and versioned so production behavior is auditable.",
  "hooks.internal.load":
    "Internal hook loader settings controlling where handler modules are discovered at startup. Use constrained load roots to reduce accidental module conflicts or shadowing.",
  "hooks.internal.load.extraDirs":
    "Additional directories searched for internal hook modules beyond default load paths. Keep this minimal and controlled to reduce accidental module shadowing.",
  "hooks.internal.installs":
    "Install metadata for internal hook modules, including source and resolved artifacts for repeatable deployments. Use this as operational provenance and avoid manual drift edits.",
};
