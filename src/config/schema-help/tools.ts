export const TOOLS_HELP: Record<string, string> = {
  tools:
    "Global tool access policy and capability configuration across web, exec, media, messaging, and elevated surfaces. Use this section to constrain risky capabilities before broad rollout.",
  "tools.allow":
    "Absolute tool allowlist that replaces profile-derived defaults for strict environments. Use this only when you intentionally run a tightly curated subset of tool capabilities.",
  "tools.deny":
    "Global tool denylist that blocks listed tools even when profile or provider rules would allow them. Use deny rules for emergency lockouts and long-term defense-in-depth.",
  "tools.web":
    "Web-tool policy grouping for search/fetch providers, limits, and fallback behavior tuning. Keep enabled settings aligned with API key availability and outbound networking policy.",
  "tools.exec":
    "Exec-tool policy grouping for shell execution host, security mode, approval behavior, and runtime bindings. Keep conservative defaults in production and tighten elevated execution paths.",
  "tools.exec.host":
    "Selects execution host strategy for shell commands, typically controlling local vs delegated execution environment. Use the safest host mode that still satisfies your automation requirements.",
  "tools.exec.security":
    "Execution security posture selector controlling sandbox/approval expectations for command execution. Keep strict security mode for untrusted prompts and relax only for trusted operator workflows.",
  "tools.exec.ask":
    "Approval strategy for when exec commands require human confirmation before running. Use stricter ask behavior in shared channels and lower-friction settings in private operator contexts.",
  "tools.exec.node":
    "Node binding configuration for exec tooling when command execution is delegated through connected nodes. Use explicit node binding only when multi-node routing is required.",
  "tools.agentToAgent":
    "Policy for allowing agent-to-agent tool calls and constraining which target agents can be reached. Keep disabled or tightly scoped unless cross-agent orchestration is intentionally enabled.",
  "tools.agentToAgent.enabled":
    "Enables the agent_to_agent tool surface so one agent can invoke another agent at runtime. Keep off in simple deployments and enable only when orchestration value outweighs complexity.",
  "tools.agentToAgent.allow":
    "Allowlist of target agent IDs permitted for agent_to_agent calls when orchestration is enabled. Use explicit allowlists to avoid uncontrolled cross-agent call graphs.",
  "tools.elevated":
    "Elevated tool access controls for privileged command surfaces that should only be reachable from trusted senders. Keep disabled unless operator workflows explicitly require elevated actions.",
  "tools.elevated.enabled":
    "Enables elevated tool execution path when sender and policy checks pass. Keep disabled in public/shared channels and enable only for trusted owner-operated contexts.",
  "tools.elevated.allowFrom":
    "Sender allow rules for elevated tools, usually keyed by channel/provider identity formats. Use narrow, explicit identities so elevated commands cannot be triggered by unintended users.",
  "tools.subagents":
    "Tool policy wrapper for spawned subagents to restrict or expand tool availability compared to parent defaults. Use this to keep delegated agent capabilities scoped to task intent.",
  "tools.subagents.tools":
    "Allow/deny tool policy applied to spawned subagent runtimes for per-subagent hardening. Keep this narrower than parent scope when subagents run semi-autonomous workflows.",
  "tools.sandbox":
    "Tool policy wrapper for sandboxed agent executions so sandbox runs can have distinct capability boundaries. Use this to enforce stronger safety in sandbox contexts.",
  "tools.sandbox.tools":
    "Allow/deny tool policy applied when agents run in sandboxed execution environments. Keep policies minimal so sandbox tasks cannot escalate into unnecessary external actions.",
  "tools.exec.applyPatch.enabled":
    "Experimental. Enables apply_patch for OpenAI models when allowed by tool policy.",
  "tools.exec.applyPatch.workspaceOnly":
    "Restrict apply_patch paths to the workspace directory (default: true). Set false to allow writing outside the workspace (dangerous).",
  "tools.exec.applyPatch.allowModels":
    'Optional allowlist of model ids (e.g. "gpt-5.2" or "openai/gpt-5.2").',
  "tools.loopDetection.enabled":
    "Enable repetitive tool-call loop detection and backoff safety checks (default: false).",
  "tools.loopDetection.historySize": "Tool history window size for loop detection (default: 30).",
  "tools.loopDetection.warningThreshold":
    "Warning threshold for repetitive patterns when detector is enabled (default: 10).",
  "tools.loopDetection.criticalThreshold":
    "Critical threshold for repetitive patterns when detector is enabled (default: 20).",
  "tools.loopDetection.globalCircuitBreakerThreshold":
    "Global no-progress breaker threshold (default: 30).",
  "tools.loopDetection.detectors.genericRepeat":
    "Enable generic repeated same-tool/same-params loop detection (default: true).",
  "tools.loopDetection.detectors.knownPollNoProgress":
    "Enable known poll tool no-progress loop detection (default: true).",
  "tools.loopDetection.detectors.pingPong": "Enable ping-pong loop detection (default: true).",
  "tools.exec.notifyOnExit":
    "When true (default), backgrounded exec sessions on exit and node exec lifecycle events enqueue a system event and request a heartbeat.",
  "tools.exec.notifyOnExitEmptySuccess":
    "When true, successful backgrounded exec exits with empty output still enqueue a completion system event (default: false).",
  "tools.exec.pathPrepend": "Directories to prepend to PATH for exec runs (gateway/sandbox).",
  "tools.exec.safeBins":
    "Allow stdin-only safe binaries to run without explicit allowlist entries.",
  "tools.exec.safeBinTrustedDirs":
    "Additional explicit directories trusted for safe-bin path checks (PATH entries are never auto-trusted).",
  "tools.exec.safeBinProfiles":
    "Optional per-binary safe-bin profiles (positional limits + allowed/denied flags).",
  "tools.profile":
    "Global tool profile name used to select a predefined tool policy baseline before applying allow/deny overrides. Use this for consistent environment posture across agents and keep profile names stable.",
  "tools.alsoAllow":
    "Extra tool allowlist entries merged on top of the selected tool profile and default policy. Keep this list small and explicit so audits can quickly identify intentional policy exceptions.",
  "tools.byProvider":
    "Per-provider tool allow/deny overrides keyed by channel/provider ID to tailor capabilities by surface. Use this when one provider needs stricter controls than global tool policy.",
  "tools.exec.approvalRunningNoticeMs":
    "Delay in milliseconds before showing an in-progress notice after an exec approval is granted. Increase to reduce flicker for fast commands, or lower for quicker operator feedback.",
  "tools.links.enabled":
    "Enable automatic link understanding pre-processing so URLs can be summarized before agent reasoning. Keep enabled for richer context, and disable when strict minimal processing is required.",
  "tools.links.maxLinks":
    "Maximum number of links expanded per turn during link understanding. Use lower values to control latency/cost in chatty threads and higher values when multi-link context is critical.",
  "tools.links.timeoutSeconds":
    "Per-link understanding timeout budget in seconds before unresolved links are skipped. Keep this bounded to avoid long stalls when external sites are slow or unreachable.",
  "tools.links.models":
    "Preferred model list for link understanding tasks, evaluated in order as fallbacks when supported. Use lightweight models first for routine summarization and heavier models only when needed.",
  "tools.links.scope":
    "Controls when link understanding runs relative to conversation context and message type. Keep scope conservative to avoid unnecessary fetches on messages where links are not actionable.",
  "tools.media.models":
    "Shared fallback model list used by media understanding tools when modality-specific model lists are not set. Keep this aligned with available multimodal providers to avoid runtime fallback churn.",
  "tools.media.concurrency":
    "Maximum number of concurrent media understanding operations per turn across image, audio, and video tasks. Lower this in resource-constrained deployments to prevent CPU/network saturation.",
  "tools.media.image.enabled":
    "Enable image understanding so attached or referenced images can be interpreted into textual context. Disable if you need text-only operation or want to avoid image-processing cost.",
  "tools.media.image.maxBytes":
    "Maximum accepted image payload size in bytes before the item is skipped or truncated by policy. Keep limits realistic for your provider caps and infrastructure bandwidth.",
  "tools.media.image.maxChars":
    "Maximum characters returned from image understanding output after model response normalization. Use tighter limits to reduce prompt bloat and larger limits for detail-heavy OCR tasks.",
  "tools.media.image.prompt":
    "Instruction template used for image understanding requests to shape extraction style and detail level. Keep prompts deterministic so outputs stay consistent across turns and channels.",
  "tools.media.image.timeoutSeconds":
    "Timeout in seconds for each image understanding request before it is aborted. Increase for high-resolution analysis and lower it for latency-sensitive operator workflows.",
  "tools.media.image.attachments":
    "Attachment handling policy for image inputs, including which message attachments qualify for image analysis. Use restrictive settings in untrusted channels to reduce unexpected processing.",
  "tools.media.image.models":
    "Ordered model preferences specifically for image understanding when you want to override shared media models. Put the most reliable multimodal model first to reduce fallback attempts.",
  "tools.media.image.scope":
    "Scope selector for when image understanding is attempted (for example only explicit requests versus broader auto-detection). Keep narrow scope in busy channels to control token and API spend.",
  "tools.media.video.enabled":
    "Enable video understanding so clips can be summarized into text for downstream reasoning and responses. Disable when processing video is out of policy or too expensive for your deployment.",
  "tools.media.video.maxBytes":
    "Maximum accepted video payload size in bytes before policy rejection or trimming occurs. Tune this to provider and infrastructure limits to avoid repeated timeout/failure loops.",
  "tools.media.video.maxChars":
    "Maximum characters retained from video understanding output to control prompt growth. Raise for dense scene descriptions and lower when concise summaries are preferred.",
  "tools.media.video.prompt":
    "Instruction template for video understanding describing desired summary granularity and focus areas. Keep this stable so output quality remains predictable across model/provider fallbacks.",
  "tools.media.video.timeoutSeconds":
    "Timeout in seconds for each video understanding request before cancellation. Use conservative values in interactive channels and longer values for offline or batch-heavy processing.",
  "tools.media.video.attachments":
    "Attachment eligibility policy for video analysis, defining which message files can trigger video processing. Keep this explicit in shared channels to prevent accidental large media workloads.",
  "tools.media.video.models":
    "Ordered model preferences specifically for video understanding before shared media fallback applies. Prioritize models with strong multimodal video support to minimize degraded summaries.",
  "tools.media.video.scope":
    "Scope selector controlling when video understanding is attempted across incoming events. Narrow scope in noisy channels, and broaden only where video interpretation is core to workflow.",
  "tools.fs.workspaceOnly":
    "Restrict filesystem tools (read/write/edit/apply_patch) to the workspace directory (default: false).",
  "tools.sessions.visibility":
    'Controls which sessions can be targeted by sessions_list/sessions_history/sessions_send. ("tree" default = current session + spawned subagent sessions; "self" = only current; "agent" = any session in the current agent id; "all" = any session; cross-agent still requires tools.agentToAgent).',
  "tools.message.allowCrossContextSend":
    "Legacy override: allow cross-context sends across all providers.",
  "tools.message.crossContext.allowWithinProvider":
    "Allow sends to other channels within the same provider (default: true).",
  "tools.message.crossContext.allowAcrossProviders":
    "Allow sends across different providers (default: false).",
  "tools.message.crossContext.marker.enabled":
    "Add a visible origin marker when sending cross-context (default: true).",
  "tools.message.crossContext.marker.prefix":
    'Text prefix for cross-context markers (supports "{channel}").',
  "tools.message.crossContext.marker.suffix":
    'Text suffix for cross-context markers (supports "{channel}").',
  "tools.message.broadcast.enabled": "Enable broadcast action (default: true).",
  "tools.web.search.enabled": "Enable the web_search tool (requires a provider API key).",
  "tools.web.search.provider":
    "Search provider id. Auto-detected from available API keys if omitted.",
  "tools.web.search.maxResults": "Number of results to return (1-10).",
  "tools.web.search.timeoutSeconds": "Timeout in seconds for web_search requests.",
  "tools.web.search.cacheTtlMinutes": "Cache TTL in minutes for web_search results.",
  "tools.web.fetch.enabled": "Enable the web_fetch tool (lightweight HTTP fetch).",
  "tools.web.fetch.maxChars": "Max characters returned by web_fetch (truncated).",
  "tools.web.fetch.maxCharsCap":
    "Hard cap for web_fetch maxChars (applies to config and tool calls).",
  "tools.web.fetch.timeoutSeconds": "Timeout in seconds for web_fetch requests.",
  "tools.web.fetch.cacheTtlMinutes": "Cache TTL in minutes for web_fetch results.",
  "tools.web.fetch.maxRedirects": "Maximum redirects allowed for web_fetch (default: 3).",
  "tools.web.fetch.userAgent": "Override User-Agent header for web_fetch requests.",
  "tools.web.fetch.readability":
    "Use Readability to extract main content from HTML (fallbacks to basic HTML cleanup).",
  "tools.web.fetch.firecrawl.enabled": "Enable Firecrawl fallback for web_fetch (if configured).",
  "tools.web.fetch.firecrawl.apiKey": "Firecrawl API key (fallback: FIRECRAWL_API_KEY env var).",
  "tools.web.fetch.firecrawl.baseUrl":
    "Firecrawl base URL (e.g. https://api.firecrawl.dev or custom endpoint).",
  "tools.web.fetch.firecrawl.onlyMainContent":
    "When true, Firecrawl returns only the main content (default: true).",
  "tools.web.fetch.firecrawl.maxAgeMs":
    "Firecrawl maxAge (ms) for cached results when supported by the API.",
  "tools.web.fetch.firecrawl.timeoutSeconds": "Timeout in seconds for Firecrawl requests.",
  "skills.load.watch":
    "Enable filesystem watching for skill-definition changes so updates can be applied without full process restart. Keep enabled in development workflows and disable in immutable production images.",
  "skills.load.watchDebounceMs":
    "Debounce window in milliseconds for coalescing rapid skill file changes before reload logic runs. Increase to reduce reload churn on frequent writes, or lower for faster edit feedback.",
};
