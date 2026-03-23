export const MEMORY_HELP: Record<string, string> = {
  memory: "Memory backend configuration (global).",
  "memory.backend":
    'Selects the global memory engine: "builtin" uses Deneb memory internals, while "qmd" uses the QMD sidecar pipeline, and "vega" uses the Vega subprocess backend. Keep "builtin" unless you intentionally operate QMD.',
  "memory.citations":
    'Controls citation visibility in replies: "auto" shows citations when useful, "on" always shows them, and "off" hides them. Keep "auto" for a balanced signal-to-noise default.',
  "memory.qmd.command":
    "Sets the executable path for the `qmd` binary used by the QMD backend (default: resolved from PATH). Use an explicit absolute path when multiple qmd installs exist or PATH differs across environments.",
  "memory.qmd.mcporter":
    "Routes QMD work through mcporter (MCP runtime) instead of spawning `qmd` for each call. Use this when cold starts are expensive on large models; keep direct process mode for simpler local setups.",
  "memory.qmd.mcporter.enabled":
    "Routes QMD through an mcporter daemon instead of spawning qmd per request, reducing cold-start overhead for larger models. Keep disabled unless mcporter is installed and configured.",
  "memory.qmd.mcporter.serverName":
    "Names the mcporter server target used for QMD calls (default: qmd). Change only when your mcporter setup uses a custom server name for qmd mcp keep-alive.",
  "memory.qmd.mcporter.startDaemon":
    "Automatically starts the mcporter daemon when mcporter-backed QMD mode is enabled (default: true). Keep enabled unless process lifecycle is managed externally by your service supervisor.",
  "memory.qmd.searchMode":
    'Selects the QMD retrieval path: "query" uses standard query flow, "search" uses search-oriented retrieval, and "vsearch" emphasizes vector retrieval. Keep default unless tuning relevance quality.',
  "memory.qmd.includeDefaultMemory":
    "Automatically indexes default memory files (MEMORY.md and memory/**/*.md) into QMD collections. Keep enabled unless you want indexing controlled only through explicit custom paths.",
  "memory.qmd.paths":
    "Adds custom directories or files to include in QMD indexing, each with an optional name and glob pattern. Use this for project-specific knowledge locations that are outside default memory paths.",
  "memory.qmd.paths.path":
    "Defines the root location QMD should scan, using an absolute path or `~`-relative path. Use stable directories so collection identity does not drift across environments.",
  "memory.qmd.paths.pattern":
    "Filters files under each indexed root using a glob pattern, with default `**/*.md`. Use narrower patterns to reduce noise and indexing cost when directories contain mixed file types.",
  "memory.qmd.paths.name":
    "Sets a stable collection name for an indexed path instead of deriving it from filesystem location. Use this when paths vary across machines but you want consistent collection identity.",
  "memory.qmd.sessions.enabled":
    "Indexes session transcripts into QMD so recall can include prior conversation content (experimental, default: false). Enable only when transcript memory is required and you accept larger index churn.",
  "memory.qmd.sessions.exportDir":
    "Overrides where sanitized session exports are written before QMD indexing. Use this when default state storage is constrained or when exports must land on a managed volume.",
  "memory.qmd.sessions.retentionDays":
    "Defines how long exported session files are kept before automatic pruning, in days (default: unlimited). Set a finite value for storage hygiene or compliance retention policies.",
  "memory.qmd.update.interval":
    "Sets how often QMD refreshes indexes from source content (duration string, default: 5m). Shorter intervals improve freshness but increase background CPU and I/O.",
  "memory.qmd.update.debounceMs":
    "Sets the minimum delay between consecutive QMD refresh attempts in milliseconds (default: 15000). Increase this if frequent file changes cause update thrash or unnecessary background load.",
  "memory.qmd.update.onBoot":
    "Runs an initial QMD update once during gateway startup (default: true). Keep enabled so recall starts from a fresh baseline; disable only when startup speed is more important than immediate freshness.",
  "memory.qmd.update.waitForBootSync":
    "Blocks startup completion until the initial boot-time QMD sync finishes (default: false). Enable when you need fully up-to-date recall before serving traffic, and keep off for faster boot.",
  "memory.qmd.update.embedInterval":
    "Sets how often QMD recomputes embeddings (duration string, default: 60m; set 0 to disable periodic embeds). Lower intervals improve freshness but increase embedding workload and cost.",
  "memory.qmd.update.commandTimeoutMs":
    "Sets timeout for QMD maintenance commands such as collection list/add in milliseconds (default: 30000). Increase when running on slower disks or remote filesystems that delay command completion.",
  "memory.qmd.update.updateTimeoutMs":
    "Sets maximum runtime for each `qmd update` cycle in milliseconds (default: 120000). Raise this for larger collections; lower it when you want quicker failure detection in automation.",
  "memory.qmd.update.embedTimeoutMs":
    "Sets maximum runtime for each `qmd embed` cycle in milliseconds (default: 120000). Increase for heavier embedding workloads or slower hardware, and lower to fail fast under tight SLAs.",
  "memory.qmd.limits.maxResults":
    "Limits how many QMD hits are returned into the agent loop for each recall request (default: 6). Increase for broader recall context, or lower to keep prompts tighter and faster.",
  "memory.qmd.limits.maxSnippetChars":
    "Caps per-result snippet length extracted from QMD hits in characters (default: 700). Lower this when prompts bloat quickly, and raise only if answers consistently miss key details.",
  "memory.qmd.limits.maxInjectedChars":
    "Caps how much QMD text can be injected into one turn across all hits. Use lower values to control prompt bloat and latency; raise only when context is consistently truncated.",
  "memory.qmd.limits.timeoutMs":
    "Sets per-query QMD search timeout in milliseconds (default: 4000). Increase for larger indexes or slower environments, and lower to keep request latency bounded.",
  "memory.qmd.scope":
    "Defines which sessions/channels are eligible for QMD recall using session.sendPolicy-style rules. Keep default direct-only scope unless you intentionally want cross-chat memory sharing.",
};
