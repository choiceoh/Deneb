export const AGENTS_HELP: Record<string, string> = {
  "agents.list.*.skills":
    "Optional allowlist of skills for this agent (omit = all skills; empty = no skills).",
  "agents.list[].skills":
    "Optional allowlist of skills for this agent (omit = all skills; empty = no skills).",
  agents:
    "Agent runtime configuration root covering defaults and explicit agent entries used for routing and execution context. Keep this section explicit so model/tool behavior stays predictable across multi-agent workflows.",
  "agents.defaults":
    "Shared default settings inherited by agents unless overridden per entry in agents.list. Use defaults to enforce consistent baseline behavior and reduce duplicated per-agent configuration.",
  "agents.list":
    "Explicit list of configured agents with IDs and optional overrides for model, tools, identity, and workspace. Keep IDs stable over time so bindings, approvals, and session routing remain deterministic.",
  "agents.list[].runtime":
    "Optional runtime descriptor for this agent. Use embedded for default Deneb execution or acp for external ACP harness defaults.",
  "agents.list[].runtime.type":
    'Runtime type for this agent: "embedded" (default Deneb runtime) or "acp" (ACP harness defaults).',
  "agents.list[].runtime.acp":
    "ACP runtime defaults for this agent when runtime.type=acp. Binding-level ACP overrides still take precedence per conversation.",
  "agents.list[].runtime.acp.agent":
    "Optional ACP harness agent id to use for this Deneb agent (for example codex, claude).",
  "agents.list[].runtime.acp.backend":
    "Optional ACP backend override for this agent's ACP sessions (falls back to global acp.backend).",
  "agents.list[].runtime.acp.mode":
    "Optional ACP session mode default for this agent (persistent or oneshot).",
  "agents.list[].runtime.acp.cwd":
    "Optional default working directory for this agent's ACP sessions.",
  "agents.list[].identity.avatar":
    "Avatar image path (relative to the agent workspace only) or a remote URL/data URL.",
  "agents.defaults.heartbeat.suppressToolErrorWarnings":
    "Suppress tool error warning payloads during heartbeat runs.",
  "agents.list[].heartbeat.suppressToolErrorWarnings":
    "Suppress tool error warning payloads during heartbeat runs.",
  "agents.defaults.sandbox.browser.network":
    "Docker network for sandbox browser containers (default: deneb-sandbox-browser). Avoid bridge if you need stricter isolation.",
  "agents.list[].sandbox.browser.network": "Per-agent override for sandbox browser Docker network.",
  "agents.defaults.sandbox.docker.dangerouslyAllowContainerNamespaceJoin":
    "DANGEROUS break-glass override that allows sandbox Docker network mode container:<id>. This joins another container namespace and weakens sandbox isolation.",
  "agents.list[].sandbox.docker.dangerouslyAllowContainerNamespaceJoin":
    "Per-agent DANGEROUS override for container namespace joins in sandbox Docker network mode.",
  "agents.defaults.sandbox.browser.cdpSourceRange":
    "Optional CIDR allowlist for container-edge CDP ingress (for example 172.21.0.1/32).",
  "agents.list[].sandbox.browser.cdpSourceRange":
    "Per-agent override for CDP source CIDR allowlist.",
  "agents.list[].tools.profile":
    "Per-agent override for tool profile selection when one agent needs a different capability baseline. Use this sparingly so policy differences across agents stay intentional and reviewable.",
  "agents.list[].tools.alsoAllow":
    "Per-agent additive allowlist for tools on top of global and profile policy. Keep narrow to avoid accidental privilege expansion on specialized agents.",
  "agents.list[].tools.byProvider":
    "Per-agent provider-specific tool policy overrides for channel-scoped capability control. Use this when a single agent needs tighter restrictions on one provider than others.",
  "agents.defaults.workspace":
    "Default workspace path exposed to agent runtime tools for filesystem context and repo-aware behavior. Set this explicitly when running from wrappers so path resolution stays deterministic.",
  "agents.defaults.bootstrapMaxChars":
    "Max characters of each workspace bootstrap file injected into the system prompt before truncation (default: 20000).",
  "agents.defaults.bootstrapTotalMaxChars":
    "Max total characters across all injected workspace bootstrap files (default: 150000).",
  "agents.defaults.bootstrapPromptTruncationWarning":
    'Inject agent-visible warning text when bootstrap files are truncated: "off", "once" (default), or "always".',
  "agents.defaults.repoRoot":
    "Optional repository root shown in the system prompt runtime line (overrides auto-detect).",
  "agents.defaults.envelopeTimezone":
    'Timezone for message envelopes ("utc", "local", "user", or an IANA timezone string).',
  "agents.defaults.envelopeTimestamp":
    'Include absolute timestamps in message envelopes ("on" or "off").',
  "agents.defaults.envelopeElapsed": 'Include elapsed time in message envelopes ("on" or "off").',
  "agents.defaults.models": "Configured model catalog (keys are full provider/model IDs).",
  "agents.defaults.memorySearch":
    "Vector search over MEMORY.md and memory/*.md (per-agent overrides supported).",
  "agents.defaults.memorySearch.enabled":
    "Master toggle for memory search indexing and retrieval behavior on this agent profile. Keep enabled for semantic recall, and disable when you want fully stateless responses.",
  "agents.defaults.memorySearch.sources":
    'Chooses which sources are indexed: "memory" reads MEMORY.md + memory files, and "sessions" includes transcript history. Keep ["memory"] unless you need recall from prior chat transcripts.',
  "agents.defaults.memorySearch.extraPaths":
    "Adds extra directories or .md files to the memory index beyond default memory files. Use this when key reference docs live elsewhere in your repo; when multimodal memory is enabled, matching image/audio files under these paths are also eligible for indexing.",
  "agents.defaults.memorySearch.multimodal":
    'Optional multimodal memory settings for indexing image and audio files from configured extra paths. Keep this off unless your embedding model explicitly supports cross-modal embeddings, and set `memorySearch.fallback` to "none" while it is enabled. Matching files are uploaded to the configured remote embedding provider during indexing.',
  "agents.defaults.memorySearch.multimodal.enabled":
    "Enables image/audio memory indexing from extraPaths. This currently requires Gemini embedding-2, keeps the default memory roots Markdown-only, disables memory-search fallback providers, and uploads matching binary content to the configured remote embedding provider.",
  "agents.defaults.memorySearch.multimodal.modalities":
    'Selects which multimodal file types are indexed from extraPaths: "image", "audio", or "all". Keep this narrow to avoid indexing large binary corpora unintentionally.',
  "agents.defaults.memorySearch.multimodal.maxFileBytes":
    "Sets the maximum bytes allowed per multimodal file before it is skipped during memory indexing. Use this to cap upload cost and indexing latency, or raise it for short high-quality audio clips.",
  "agents.defaults.memorySearch.experimental.sessionMemory":
    "Indexes session transcripts into memory search so responses can reference prior chat turns. Keep this off unless transcript recall is needed, because indexing cost and storage usage both increase.",
  "agents.defaults.memorySearch.provider":
    'Selects the embedding backend used to build/query memory vectors: "openai", "gemini", "voyage", "mistral", or "local". Keep your most reliable provider here and configure fallback for resilience.',
  "agents.defaults.memorySearch.model":
    "Embedding model override used by the selected memory provider when a non-default model is required. Set this only when you need explicit recall quality/cost tuning beyond provider defaults.",
  "agents.defaults.memorySearch.outputDimensionality":
    "Gemini embedding-2 only: chooses the output vector size for memory embeddings. Use 768, 1536, or 3072 (default), and expect a full reindex when you change it because stored vector dimensions must stay consistent.",
  "agents.defaults.memorySearch.remote.baseUrl":
    "Overrides the embedding API endpoint, such as an OpenAI-compatible proxy or custom Gemini base URL. Use this only when routing through your own gateway or vendor endpoint; keep provider defaults otherwise.",
  "agents.defaults.memorySearch.remote.apiKey":
    "Supplies a dedicated API key for remote embedding calls used by memory indexing and query-time embeddings. Use this when memory embeddings should use different credentials than global defaults or environment variables.",
  "agents.defaults.memorySearch.remote.headers":
    "Adds custom HTTP headers to remote embedding requests, merged with provider defaults. Use this for proxy auth and tenant routing headers, and keep values minimal to avoid leaking sensitive metadata.",
  "agents.defaults.memorySearch.remote.batch.enabled":
    "Enables provider batch APIs for embedding jobs when supported (OpenAI/Gemini), improving throughput on larger index runs. Keep this enabled unless debugging provider batch failures or running very small workloads.",
  "agents.defaults.memorySearch.remote.batch.wait":
    "Waits for batch embedding jobs to fully finish before the indexing operation completes. Keep this enabled for deterministic indexing state; disable only if you accept delayed consistency.",
  "agents.defaults.memorySearch.remote.batch.concurrency":
    "Limits how many embedding batch jobs run at the same time during indexing (default: 2). Increase carefully for faster bulk indexing, but watch provider rate limits and queue errors.",
  "agents.defaults.memorySearch.remote.batch.pollIntervalMs":
    "Controls how often the system polls provider APIs for batch job status in milliseconds (default: 2000). Use longer intervals to reduce API chatter, or shorter intervals for faster completion detection.",
  "agents.defaults.memorySearch.remote.batch.timeoutMinutes":
    "Sets the maximum wait time for a full embedding batch operation in minutes (default: 60). Increase for very large corpora or slower providers, and lower it to fail fast in automation-heavy flows.",
  "agents.defaults.memorySearch.local.modelPath":
    "Specifies the local embedding model source for local memory search, such as a GGUF file path or `hf:` URI. Use this only when provider is `local`, and verify model compatibility before large index rebuilds.",
  "agents.defaults.memorySearch.fallback":
    'Backup provider used when primary embeddings fail: "openai", "gemini", "voyage", "mistral", "local", or "none". Set a real fallback for production reliability; use "none" only if you prefer explicit failures.',
  "agents.defaults.memorySearch.store.path":
    "Sets where the SQLite memory index is stored on disk for each agent. Keep the default `~/.deneb/memory/{agentId}.sqlite` unless you need custom storage placement or backup policy alignment.",
  "agents.defaults.memorySearch.store.vector.enabled":
    "Enables the sqlite-vec extension used for vector similarity queries in memory search (default: true). Keep this enabled for normal semantic recall; disable only for debugging or fallback-only operation.",
  "agents.defaults.memorySearch.store.vector.extensionPath":
    "Overrides the auto-discovered sqlite-vec extension library path (`.dylib`, `.so`, or `.dll`). Use this when your runtime cannot find sqlite-vec automatically or you pin a known-good build.",
  "agents.defaults.memorySearch.chunking.tokens":
    "Chunk size in tokens used when splitting memory sources before embedding/indexing. Increase for broader context per chunk, or lower to improve precision on pinpoint lookups.",
  "agents.defaults.memorySearch.chunking.overlap":
    "Token overlap between adjacent memory chunks to preserve context continuity near split boundaries. Use modest overlap to reduce boundary misses without inflating index size too aggressively.",
  "agents.defaults.memorySearch.query.maxResults":
    "Maximum number of memory hits returned from search before downstream reranking and prompt injection. Raise for broader recall, or lower for tighter prompts and faster responses.",
  "agents.defaults.memorySearch.query.minScore":
    "Minimum relevance score threshold for including memory results in final recall output. Increase to reduce weak/noisy matches, or lower when you need more permissive retrieval.",
  "agents.defaults.memorySearch.query.hybrid.enabled":
    "Combines BM25 keyword matching with vector similarity for better recall on mixed exact + semantic queries. Keep enabled unless you are isolating ranking behavior for troubleshooting.",
  "agents.defaults.memorySearch.query.hybrid.vectorWeight":
    "Controls how strongly semantic similarity influences hybrid ranking (0-1). Increase when paraphrase matching matters more than exact terms; decrease for stricter keyword emphasis.",
  "agents.defaults.memorySearch.query.hybrid.textWeight":
    "Controls how strongly BM25 keyword relevance influences hybrid ranking (0-1). Increase for exact-term matching; decrease when semantic matches should rank higher.",
  "agents.defaults.memorySearch.query.hybrid.candidateMultiplier":
    "Expands the candidate pool before reranking (default: 4). Raise this for better recall on noisy corpora, but expect more compute and slightly slower searches.",
  "agents.defaults.memorySearch.query.hybrid.mmr.enabled":
    "Adds MMR reranking to diversify results and reduce near-duplicate snippets in a single answer window. Enable when recall looks repetitive; keep off for strict score ordering.",
  "agents.defaults.memorySearch.query.hybrid.mmr.lambda":
    "Sets MMR relevance-vs-diversity balance (0 = most diverse, 1 = most relevant, default: 0.7). Lower values reduce repetition; higher values keep tightly relevant but may duplicate.",
  "agents.defaults.memorySearch.query.hybrid.temporalDecay.enabled":
    "Applies recency decay so newer memory can outrank older memory when scores are close. Enable when timeliness matters; keep off for timeless reference knowledge.",
  "agents.defaults.memorySearch.query.hybrid.temporalDecay.halfLifeDays":
    "Controls how fast older memory loses rank when temporal decay is enabled (half-life in days, default: 30). Lower values prioritize recent context more aggressively.",
  "agents.defaults.memorySearch.cache.enabled":
    "Caches computed chunk embeddings in SQLite so reindexing and incremental updates run faster (default: true). Keep this enabled unless investigating cache correctness or minimizing disk usage.",
  "agents.defaults.memorySearch.cache.maxEntries":
    "Sets a best-effort upper bound on cached embeddings kept in SQLite for memory search. Use this when controlling disk growth matters more than peak reindex speed.",
  "agents.defaults.memorySearch.sync.onSessionStart":
    "Triggers a memory index sync when a session starts so early turns see fresh memory content. Keep enabled when startup freshness matters more than initial turn latency.",
  "agents.defaults.memorySearch.sync.onSearch":
    "Uses lazy sync by scheduling reindex on search after content changes are detected. Keep enabled for lower idle overhead, or disable if you require pre-synced indexes before any query.",
  "agents.defaults.memorySearch.sync.watch":
    "Watches memory files and schedules index updates from file-change events (chokidar). Enable for near-real-time freshness; disable on very large workspaces if watch churn is too noisy.",
  "agents.defaults.memorySearch.sync.watchDebounceMs":
    "Debounce window in milliseconds for coalescing rapid file-watch events before reindex runs. Increase to reduce churn on frequently-written files, or lower for faster freshness.",
  "agents.defaults.memorySearch.sync.sessions.deltaBytes":
    "Requires at least this many newly appended bytes before session transcript changes trigger reindex (default: 100000). Increase to reduce frequent small reindexes, or lower for faster transcript freshness.",
  "agents.defaults.memorySearch.sync.sessions.deltaMessages":
    "Requires at least this many appended transcript messages before reindex is triggered (default: 50). Lower this for near-real-time transcript recall, or raise it to reduce indexing churn.",
  "agents.defaults.memorySearch.sync.sessions.postCompactionForce":
    "Forces a session memory-search reindex after compaction-triggered transcript updates (default: true). Keep enabled when compacted summaries must be immediately searchable, or disable to reduce write-time indexing pressure.",
  "agents.list.*.identity.avatar":
    "Agent avatar (workspace-relative path, http(s) URL, or data URI).",
  "agents.defaults.model.primary": "Primary model (provider/model).",
  "agents.defaults.model.fallbacks":
    "Ordered fallback models (provider/model). Used when the primary model fails.",
  "agents.defaults.imageModel.primary":
    "Optional image model (provider/model) used when the primary model lacks image input.",
  "agents.defaults.imageModel.fallbacks": "Ordered fallback image models (provider/model).",
  "agents.defaults.imageGenerationModel.primary":
    "Optional image-generation model (provider/model) used by the shared image generation capability.",
  "agents.defaults.imageGenerationModel.fallbacks":
    "Ordered fallback image-generation models (provider/model).",
  "agents.defaults.pdfModel.primary":
    "Optional PDF model (provider/model) for the PDF analysis tool. Defaults to imageModel, then session model.",
  "agents.defaults.pdfModel.fallbacks": "Ordered fallback PDF models (provider/model).",
  "agents.defaults.pdfMaxBytesMb":
    "Maximum PDF file size in megabytes for the PDF tool (default: 10).",
  "agents.defaults.pdfMaxPages":
    "Maximum number of PDF pages to process for the PDF tool (default: 20).",
  "agents.defaults.imageMaxDimensionPx":
    "Max image side length in pixels when sanitizing transcript/tool-result image payloads (default: 1200).",
  "agents.defaults.cliBackends": "Optional CLI backends for text-only fallback (claude-cli, etc.).",
  "agents.defaults.maxHistoryTurns":
    "Maximum number of user turns to keep in context when no channel-specific historyLimit is configured. Prevents unbounded context growth in long-running sessions. Default: 100. Set 0 to disable the limit entirely.",
  "agents.defaults.compaction":
    "Compaction tuning for when context nears token limits, including history share, reserve headroom, and pre-compaction memory flush behavior. Use this when long-running sessions need stable continuity under tight context windows.",
  "agents.defaults.compaction.mode":
    'System-managed: always "safeguard". This field is accepted in config for backward compatibility but ignored at runtime.',
  "agents.defaults.compaction.reserveTokens":
    "Token headroom reserved for reply generation and tool output after compaction runs. Use higher reserves for verbose/tool-heavy sessions, and lower reserves when maximizing retained history matters more.",
  "agents.defaults.compaction.keepRecentTokens":
    "Minimum token budget preserved from the most recent conversation window during compaction. Use higher values to protect immediate context continuity and lower values to keep more long-tail history.",
  "agents.defaults.compaction.reserveTokensFloor":
    "Minimum floor enforced for reserveTokens in Pi compaction paths (0 disables the floor guard). Use a non-zero floor to avoid over-aggressive compression under fluctuating token estimates.",
  "agents.defaults.compaction.maxHistoryShare":
    "System-managed: fixed at 0.5. This field is accepted in config for backward compatibility but ignored at runtime.",
  "agents.defaults.compaction.identifierPolicy":
    'Identifier-preservation policy for compaction summaries: "strict" prepends built-in opaque-identifier retention guidance (default), "off" disables this prefix, and "custom" uses identifierInstructions. Keep "strict" unless you have a specific compatibility need.',
  "agents.defaults.compaction.identifierInstructions":
    'Custom identifier-preservation instruction text used when identifierPolicy="custom". Keep this explicit and safety-focused so compaction summaries do not rewrite opaque IDs, URLs, hosts, or ports.',
  "agents.defaults.compaction.recentTurnsPreserve":
    "System-managed: fixed at 3. This field is accepted in config for backward compatibility but ignored at runtime.",
  "agents.defaults.compaction.qualityGuard":
    "System-managed: always enabled with maxRetries=1. This field is accepted in config for backward compatibility but ignored at runtime.",
  "agents.defaults.compaction.qualityGuard.enabled":
    "System-managed: always true. This field is accepted in config for backward compatibility but ignored at runtime.",
  "agents.defaults.compaction.qualityGuard.maxRetries":
    "System-managed: fixed at 1. This field is accepted in config for backward compatibility but ignored at runtime.",
  "agents.defaults.compaction.postIndexSync":
    'Controls post-compaction session memory reindex mode: "off", "async", or "await" (default: "async"). Use "await" for strongest freshness, "async" for lower compaction latency, and "off" only when session-memory sync is handled elsewhere.',
  "agents.defaults.compaction.postCompactionSections":
    'AGENTS.md H2/H3 section names re-injected after compaction so the agent reruns critical startup guidance. Leave unset to use "Session Startup"/"Red Lines" with legacy fallback to "Every Session"/"Safety"; set to [] to disable reinjection entirely.',
  "agents.defaults.compaction.timeoutSeconds":
    "Maximum time in seconds allowed for a single compaction operation before it is aborted (default: 900). Increase this for very large sessions that need more time to summarize, or decrease it to fail faster on unresponsive models.",
  "agents.defaults.compaction.model":
    "Optional provider/model override used only for compaction summarization. Set this when you want compaction to run on a different model than the session default, and leave it unset to keep using the primary agent model.",
  "agents.defaults.compaction.truncateAfterCompaction":
    "When enabled, rewrites the session JSONL file after compaction to remove entries that were summarized. Prevents unbounded file growth in long-running sessions with many compaction cycles. Default: true.",
  "agents.defaults.compaction.memoryFlush":
    "Pre-compaction memory flush settings that run an agentic memory write before heavy compaction. Keep enabled for long sessions so salient context is persisted before aggressive trimming.",
  "agents.defaults.compaction.memoryFlush.enabled":
    "Enables pre-compaction memory flush before the runtime performs stronger history reduction near token limits. Keep enabled unless you intentionally disable memory side effects in constrained environments.",
  "agents.defaults.compaction.memoryFlush.softThresholdTokens":
    "Threshold distance to compaction (in tokens) that triggers pre-compaction memory flush execution. Use earlier thresholds for safer persistence, or tighter thresholds for lower flush frequency.",
  "agents.defaults.compaction.memoryFlush.forceFlushTranscriptBytes":
    'Forces pre-compaction memory flush when transcript file size reaches this threshold (bytes or strings like "2mb"). Use this to prevent long-session hangs even when token counters are stale; set to 0 to disable.',
  "agents.defaults.compaction.memoryFlush.prompt":
    "User-prompt template used for the pre-compaction memory flush turn when generating memory candidates. Use this only when you need custom extraction instructions beyond the default memory flush behavior.",
  "agents.defaults.compaction.memoryFlush.systemPrompt":
    "System-prompt override for the pre-compaction memory flush turn to control extraction style and safety constraints. Use carefully so custom instructions do not reduce memory quality or leak sensitive context.",
  "agents.defaults.embeddedPi":
    "Embedded Pi runner hardening controls for how workspace-local Pi settings are trusted and applied in Deneb sessions.",
  "agents.defaults.embeddedPi.projectSettingsPolicy":
    'How embedded Pi handles workspace-local `.pi/config/settings.json`: "sanitize" (default) strips shellPath/shellCommandPrefix, "ignore" disables project settings entirely, and "trusted" applies project settings as-is.',
  "agents.defaults.humanDelay.mode": 'Delay style for block replies ("off", "natural", "custom").',
  "agents.defaults.humanDelay.minMs": "Minimum delay in ms for custom humanDelay (default: 800).",
  "agents.defaults.humanDelay.maxMs": "Maximum delay in ms for custom humanDelay (default: 2500).",
  "agents.defaults.heartbeat.directPolicy":
    'Controls whether heartbeat delivery may target direct/DM chats: "allow" (default) permits DM delivery and "block" suppresses direct-target sends.',
  "agents.list.*.heartbeat.directPolicy":
    'Per-agent override for heartbeat direct/DM delivery policy; use "block" for agents that should only send heartbeat alerts to non-DM destinations.',
};
