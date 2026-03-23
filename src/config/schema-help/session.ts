export const SESSION_HELP: Record<string, string> = {
  session:
    "Global session routing, reset, delivery policy, and maintenance controls for conversation history behavior. Keep defaults unless you need stricter isolation, retention, or delivery constraints.",
  "session.scope":
    'Sets base session grouping strategy: "per-sender" isolates by sender and "global" shares one session per channel context. Keep "per-sender" for safer multi-user behavior unless deliberate shared context is required.',
  "session.dmScope":
    'DM session scoping: "main" keeps continuity, while "per-peer", "per-channel-peer", and "per-account-channel-peer" increase isolation. Use isolated modes for shared inboxes or multi-account deployments.',
  "session.identityLinks":
    "Maps canonical identities to provider-prefixed peer IDs so equivalent users resolve to one DM thread (example: telegram:123456). Use this when the same human appears across multiple channels or accounts.",
  "session.resetTriggers":
    "Lists message triggers that force a session reset when matched in inbound content. Use sparingly for explicit reset phrases so context is not dropped unexpectedly during normal conversation.",
  "session.idleMinutes":
    "Applies a legacy idle reset window in minutes for session reuse behavior across inactivity gaps. Use this only for compatibility and prefer structured reset policies under session.reset/session.resetByType.",
  "session.reset":
    "Defines the default reset policy object used when no type-specific or channel-specific override applies. Set this first, then layer resetByType or resetByChannel only where behavior must differ.",
  "session.reset.mode":
    'Selects reset strategy: "daily" resets at a configured hour and "idle" resets after inactivity windows. Keep one clear mode per policy to avoid surprising context turnover patterns.',
  "session.reset.atHour":
    "Sets local-hour boundary (0-23) for daily reset mode so sessions roll over at predictable times. Use with mode=daily and align to operator timezone expectations for human-readable behavior.",
  "session.reset.idleMinutes":
    "Sets inactivity window before reset for idle mode and can also act as secondary guard with daily mode. Use larger values to preserve continuity or smaller values for fresher short-lived threads.",
  "session.resetByType":
    "Overrides reset behavior by chat type (direct, group, thread) when defaults are not sufficient. Use this when group/thread traffic needs different reset cadence than direct messages.",
  "session.resetByType.direct":
    "Defines reset policy for direct chats and supersedes the base session.reset configuration for that type. Use this as the canonical direct-message override instead of the legacy dm alias.",
  "session.resetByType.dm":
    "Deprecated alias for direct reset behavior kept for backward compatibility with older configs. Use session.resetByType.direct instead so future tooling and validation remain consistent.",
  "session.resetByType.group":
    "Defines reset policy for group chat sessions where continuity and noise patterns differ from DMs. Use shorter idle windows for busy groups if context drift becomes a problem.",
  "session.resetByType.thread":
    "Defines reset policy for thread-scoped sessions, including focused channel thread workflows. Use this when thread sessions should expire faster or slower than other chat types.",
  "session.resetByChannel":
    "Provides channel-specific reset overrides keyed by provider/channel id for fine-grained behavior control. Use this only when one channel needs exceptional reset behavior beyond type-level policies.",
  "session.store":
    "Sets the session storage file path used to persist session records across restarts. Use an explicit path only when you need custom disk layout, backup routing, or mounted-volume storage.",
  "session.typingIntervalSeconds":
    "Controls interval for repeated typing indicators while replies are being prepared in typing-capable channels. Increase to reduce chatty updates or decrease for more active typing feedback.",
  "session.typingMode":
    'Controls typing behavior timing: "never", "instant", "thinking", or "message" based emission points. Keep conservative modes in high-volume channels to avoid unnecessary typing noise.',
  "session.parentForkMaxTokens":
    "Maximum parent-session token count allowed for thread/session inheritance forking. If the parent exceeds this, Deneb starts a fresh thread session instead of forking; set 0 to disable this protection.",
  "session.mainKey":
    'Overrides the canonical main session key used for continuity when dmScope or routing logic points to "main". Use a stable value only if you intentionally need custom session anchoring.',
  "session.sendPolicy":
    "Controls cross-session send permissions using allow/deny rules evaluated against channel, chatType, and key prefixes. Use this to fence where session tools can deliver messages in complex environments.",
  "session.sendPolicy.default":
    'Sets fallback action when no sendPolicy rule matches: "allow" or "deny". Keep "allow" for simpler setups, or choose "deny" when you require explicit allow rules for every destination.',
  "session.sendPolicy.rules":
    'Ordered allow/deny rules evaluated before the default action, for example `{ action: "deny", match: { channel: "telegram" } }`. Put most specific rules first so broad rules do not shadow exceptions.',
  "session.sendPolicy.rules[].action":
    'Defines rule decision as "allow" or "deny" when the corresponding match criteria are satisfied. Use deny-first ordering when enforcing strict boundaries with explicit allow exceptions.',
  "session.sendPolicy.rules[].match":
    "Defines optional rule match conditions that can combine channel, chatType, and key-prefix constraints. Keep matches narrow so policy intent stays readable and debugging remains straightforward.",
  "session.sendPolicy.rules[].match.channel":
    "Matches rule application to a specific channel/provider id (for example telegram). Use this when one channel should permit or deny delivery independently of others.",
  "session.sendPolicy.rules[].match.chatType":
    "Matches rule application to chat type (direct, group, thread) so behavior varies by conversation form. Use this when DM and group destinations require different safety boundaries.",
  "session.sendPolicy.rules[].match.keyPrefix":
    "Matches a normalized session-key prefix after internal key normalization steps in policy consumers. Use this for general prefix controls, and prefer rawKeyPrefix when exact full-key matching is required.",
  "session.sendPolicy.rules[].match.rawKeyPrefix":
    "Matches the raw, unnormalized session-key prefix for exact full-key policy targeting. Use this when normalized keyPrefix is too broad and you need agent-prefixed or transport-specific precision.",
  "session.agentToAgent":
    "Groups controls for inter-agent session exchanges, including loop prevention limits on reply chaining. Keep defaults unless you run advanced agent-to-agent automation with strict turn caps.",
  "session.agentToAgent.maxPingPongTurns":
    "Max reply-back turns between requester and target agents during agent-to-agent exchanges (0-5). Use lower values to hard-limit chatter loops and preserve predictable run completion.",
  "session.threadBindings":
    "Shared defaults for thread-bound session routing behavior across providers that support thread focus workflows. Configure global defaults here and override per channel only when behavior differs.",
  "session.threadBindings.enabled":
    "Global master switch for thread-bound session routing features and focused thread delivery behavior. Keep enabled for modern thread workflows unless you need to disable thread binding globally.",
  "session.threadBindings.idleHours":
    "Default inactivity window in hours for thread-bound sessions across providers/channels (0 disables idle auto-unfocus). Default: 24.",
  "session.threadBindings.maxAgeHours":
    "Optional hard max age in hours for thread-bound sessions across providers/channels (0 disables hard cap). Default: 0.",
  "session.maintenance":
    "Automatic session-store maintenance controls for pruning age, entry caps, and file rotation behavior. Start in warn mode to observe impact, then enforce once thresholds are tuned.",
  "session.maintenance.mode":
    'Determines whether maintenance policies are only reported ("warn") or actively applied ("enforce"). Keep "warn" during rollout and switch to "enforce" after validating safe thresholds.',
  "session.maintenance.pruneAfter":
    "Removes entries older than this duration (for example `30d` or `12h`) during maintenance passes. Use this as the primary age-retention control and align it with data retention policy.",
  "session.maintenance.pruneDays":
    "Deprecated age-retention field kept for compatibility with legacy configs using day counts. Use session.maintenance.pruneAfter instead so duration syntax and behavior are consistent.",
  "session.maintenance.maxEntries":
    "Caps total session entry count retained in the store to prevent unbounded growth over time. Use lower limits for constrained environments, or higher limits when longer history is required.",
  "session.maintenance.rotateBytes":
    "Rotates the session store when file size exceeds a threshold such as `10mb` or `1gb`. Use this to bound single-file growth and keep backup/restore operations manageable.",
  "session.maintenance.resetArchiveRetention":
    "Retention for reset transcript archives (`*.reset.<timestamp>`). Accepts a duration (for example `30d`), or `false` to disable cleanup. Defaults to pruneAfter so reset artifacts do not grow forever.",
  "session.maintenance.maxDiskBytes":
    "Optional per-agent sessions-directory disk budget (for example `500mb`). Use this to cap session storage per agent; when exceeded, warn mode reports pressure and enforce mode performs oldest-first cleanup.",
  "session.maintenance.highWaterBytes":
    "Target size after disk-budget cleanup (high-water mark). Defaults to 80% of maxDiskBytes; set explicitly for tighter reclaim behavior on constrained disks.",
  cron: "Global scheduler settings for stored cron jobs, run concurrency, delivery fallback, and run-session retention. Keep defaults unless you are scaling job volume or integrating external webhook receivers.",
  "cron.enabled":
    "Enables cron job execution for stored schedules managed by the gateway. Keep enabled for normal reminder/automation flows, and disable only to pause all cron execution without deleting jobs.",
  "cron.store":
    "Path to the cron job store file used to persist scheduled jobs across restarts. Set an explicit path only when you need custom storage layout, backups, or mounted volumes.",
  "cron.maxConcurrentRuns":
    "Limits how many cron jobs can execute at the same time when multiple schedules fire together. Use lower values to protect CPU/memory under heavy automation load, or raise carefully for higher throughput.",
  "cron.retry":
    "Overrides the default retry policy for one-shot jobs when they fail with transient errors (rate limit, overloaded, network, server_error). Omit to use defaults: maxAttempts 3, backoffMs [30000, 60000, 300000], retry all transient types.",
  "cron.retry.maxAttempts":
    "Max retries for one-shot jobs on transient errors before permanent disable (default: 3).",
  "cron.retry.backoffMs":
    "Backoff delays in ms for each retry attempt (default: [30000, 60000, 300000]). Use shorter values for faster retries.",
  "cron.retry.retryOn":
    "Error types to retry: rate_limit, overloaded, network, timeout, server_error. Use to restrict which errors trigger retries; omit to retry all transient types.",
  "cron.webhook":
    'Deprecated legacy fallback webhook URL used only for old jobs with `notify=true`. Migrate to per-job delivery using `delivery.mode="webhook"` plus `delivery.to`, and avoid relying on this global field.',
  "cron.webhookToken":
    "Bearer token attached to cron webhook POST deliveries when webhook mode is used. Prefer secret/env substitution and rotate this token regularly if shared webhook endpoints are internet-reachable.",
  "cron.sessionRetention":
    "Controls how long completed cron run sessions are kept before pruning (`24h`, `7d`, `1h30m`, or `false` to disable pruning; default: `24h`). Use shorter retention to reduce storage growth on high-frequency schedules.",
  "cron.runLog":
    "Pruning controls for per-job cron run history files under `cron/runs/<jobId>.jsonl`, including size and line retention.",
  "cron.runLog.maxBytes":
    "Maximum bytes per cron run-log file before pruning rewrites to the last keepLines entries (for example `2mb`, default `2000000`).",
  "cron.runLog.keepLines":
    "How many trailing run-log lines to retain when a file exceeds maxBytes (default `2000`). Increase for longer forensic history or lower for smaller disks.",
};
