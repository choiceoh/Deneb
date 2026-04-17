# Graph Report - .  (2026-04-18)

## Corpus Check
- Large corpus: 719 files · ~470,089 words. Semantic extraction will be expensive (many Claude tokens). Consider running on a subfolder, or use --no-semantic to run AST-only.

## Summary
- 6344 nodes · 17595 edges · 109 communities detected
- Extraction: 46% EXTRACTED · 54% INFERRED · 0% AMBIGUOUS · INFERRED: 9453 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Htmlmd|Htmlmd]]
- [[_COMMUNITY_Coremarkdown|Coremarkdown]]
- [[_COMMUNITY_Skills|Skills]]
- [[_COMMUNITY_Wiki|Wiki]]
- [[_COMMUNITY_Tasks|Tasks]]
- [[_COMMUNITY_Session|Session]]
- [[_COMMUNITY_Telegram|Telegram]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Config|Config]]
- [[_COMMUNITY_Tools|Tools]]
- [[_COMMUNITY_Skills|Skills]]
- [[_COMMUNITY_Dev|Dev]]
- [[_COMMUNITY_Rpc|Rpc]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Handlers|Handlers]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Acp|Acp]]
- [[_COMMUNITY_Provider|Provider]]
- [[_COMMUNITY_Polaris|Polaris]]
- [[_COMMUNITY_Events|Events]]
- [[_COMMUNITY_Autonomous|Autonomous]]
- [[_COMMUNITY_Compaction|Compaction]]
- [[_COMMUNITY_Process|Process]]
- [[_COMMUNITY_Web|Web]]
- [[_COMMUNITY_Toolreg|Toolreg]]
- [[_COMMUNITY_Gmail|Gmail]]
- [[_COMMUNITY_Typing|Typing]]
- [[_COMMUNITY_Prompt|Prompt]]
- [[_COMMUNITY_Subagent|Subagent]]
- [[_COMMUNITY_Handlertask|Handlertask]]
- [[_COMMUNITY_Claude|Claude]]
- [[_COMMUNITY_Session|Session]]
- [[_COMMUNITY_Skills|Skills]]
- [[_COMMUNITY_Session|Session]]
- [[_COMMUNITY_Agent|Agent]]
- [[_COMMUNITY_Config|Config]]
- [[_COMMUNITY_Telegram|Telegram]]
- [[_COMMUNITY_Provider|Provider]]
- [[_COMMUNITY_Llm|Llm]]
- [[_COMMUNITY_Httpretry|Httpretry]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Types|Types]]
- [[_COMMUNITY_Wiki|Wiki]]
- [[_COMMUNITY_Agentlog|Agentlog]]
- [[_COMMUNITY_Provider|Provider]]
- [[_COMMUNITY_Healthcheck|Healthcheck]]
- [[_COMMUNITY_Types|Types]]
- [[_COMMUNITY_Toolctx|Toolctx]]
- [[_COMMUNITY_Chatport|Chatport]]
- [[_COMMUNITY_Polaris|Polaris]]
- [[_COMMUNITY_Agent|Agent]]
- [[_COMMUNITY_Protocol|Protocol]]
- [[_COMMUNITY_Claude|Claude]]
- [[_COMMUNITY_Autoreply|Autoreply]]
- [[_COMMUNITY_Changelog|Changelog]]
- [[_COMMUNITY_Tasks|Tasks]]
- [[_COMMUNITY_Testdata|Testdata]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Autonomous|Autonomous]]
- [[_COMMUNITY_Autonomous|Autonomous]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Claude|Claude]]
- [[_COMMUNITY_Tools|Tools]]
- [[_COMMUNITY_Agent|Agent]]
- [[_COMMUNITY_Agent|Agent]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Claude|Claude]]
- [[_COMMUNITY_Scripts|Scripts]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Chat|Chat]]
- [[_COMMUNITY_Tools|Tools]]
- [[_COMMUNITY_Timeouts|Timeouts]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Cron|Cron]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Server|Server]]
- [[_COMMUNITY_Rpc|Rpc]]
- [[_COMMUNITY_System|System]]
- [[_COMMUNITY_Process|Process]]
- [[_COMMUNITY_Process|Process]]
- [[_COMMUNITY_Process|Process]]
- [[_COMMUNITY_Genesis|Genesis]]
- [[_COMMUNITY_Protocol|Protocol]]
- [[_COMMUNITY_Protocol|Protocol]]
- [[_COMMUNITY_Changelog|Changelog]]
- [[_COMMUNITY_Changelog|Changelog]]
- [[_COMMUNITY_Changelog|Changelog]]
- [[_COMMUNITY_Readme|Readme]]
- [[_COMMUNITY_Testdata|Testdata]]
- [[_COMMUNITY_Testdata|Testdata]]
- [[_COMMUNITY_Testdata|Testdata]]
- [[_COMMUNITY_Integration|Integration]]

## God Nodes (most connected - your core abstractions)
1. `Append()` - 486 edges
2. `contains()` - 285 edges
3. `Unmarshal()` - 205 edges
4. `run()` - 189 edges
5. `Must()` - 125 edges
6. `New()` - 108 edges
7. `WriteFile()` - 107 edges
8. `NoError()` - 82 edges
9. `Load()` - 82 edges
10. `NewManager()` - 68 edges

## Surprising Connections (you probably didn't know these)
- `TestTokenestIntegration()` --calls--> `run()`  [INFERRED]
  gateway-go/internal/pipeline/chat/prompt/budget_test.go → scripts/dev/quality-test.py
- `User Profile: Oh Sunteack` --semantically_similar_to--> `Default USER.md (Clawdributors)`  [INFERRED] [semantically similar]
  USER.md → docs/reference/templates/USER.md
- `SOUL Core Truths` --semantically_similar_to--> `SOUL.md Template`  [INFERRED] [semantically similar]
  SOUL.md → docs/reference/templates/SOUL.md
- `AGENTS Session Start Protocol` --semantically_similar_to--> `AGENTS.md Workspace Template`  [INFERRED] [semantically similar]
  AGENTS.md → docs/reference/templates/AGENTS.md
- `Core Skills Roster` --semantically_similar_to--> `Default Skills Roster`  [INFERRED] [semantically similar]
  AGENTS.md → docs/reference/AGENTS.default.md

## Hyperedges (group relationships)
- **Session Start Triad: SOUL + USER + MEMORY** — soul_core_truths, user_oh_sunteack, agents_memory_system, agents_session_start [EXTRACTED 1.00]
- **Telegram Vibe Coder Pipeline** — concept_vibe_coder, gateway_go_telegram_vibe_coder, gateway_go_telegram_reply_pipeline, gateway_go_system_prompt_build, claude_korean_first [EXTRACTED 1.00]
- **Workspace Bootstrap Template Set** — docs_bootstrap_template, docs_agents_template, docs_soul_template, docs_user_template, docs_tools_template, docs_heartbeat_template, docs_identity_c3po [EXTRACTED 1.00]
- **Skill Lifecycle Trio (Factory -> Creator -> Evolution)** — skill_skill_factory, skill_skill_creator, skill_skill_evolution, claude_skill_lifecycle [EXTRACTED 1.00]
- **Progressive Loading / Prompt Cache Design Stack** — claude_progressive_loading, claude_prompt_cache_design, claude_skill_frontmatter_spec, skill_skill_creator_progressive_disclosure [INFERRED 0.90]
- **Morning Letter Data Pipeline** — skill_morning_letter, skill_morning_letter_tool_dependency, skill_morning_letter_sections, skill_summarize [EXTRACTED 1.00]

## Communities

### Community 0 - "Chat"
Cohesion: 0.01
Nodes (305): NewAbortTracker(), newTestLLMClient(), sseResponse(), sseToolResponse(), TestRunAgent_Abort(), TestRunAgent_MaxTurns(), TestRunAgent_SimpleTextResponse(), TestRunAgent_Timeout() (+297 more)

### Community 1 - "Htmlmd"
Cohesion: 0.01
Nodes (380): formatReport(), loadPrompt(), TestFormatEmailForAnalysis(), TestFormatEmailForAnalysis_LongBody(), TestFormatReport_EscapesHTML(), TestFormatReport_HTML(), TestLoadPrompt_CustomFile(), TestLoadPrompt_Default() (+372 more)

### Community 2 - "Coremarkdown"
Cohesion: 0.01
Nodes (261): filterByPriority(), NewFragment(), removeShrinkableSmallestFirst(), shrinkByPriority(), shrinkContent(), sumTokens(), makeContent(), TestAssemble() (+253 more)

### Community 3 - "Skills"
Cohesion: 0.01
Nodes (241): agentStatus(), cronGet(), cronList(), cronUnregister(), ExtendedDeps, ExtendedMethods(), processExec(), processGet() (+233 more)

### Community 4 - "Wiki"
Cohesion: 0.01
Nodes (185): FileCache, ApprovalRule, ApprovalsFile, CreateRequestParams, Decision, Snapshot, TurnSourceInfo, LRU[K, V] (+177 more)

### Community 5 - "Tasks"
Cohesion: 0.02
Nodes (148): checkTimestamps(), RunAudit(), taskReferenceAt(), BaseHTTPRequestHandler, cacheKey(), newResponseCache(), chat_send_and_wait(), main() (+140 more)

### Community 6 - "Session"
Cohesion: 0.01
Nodes (183): shouldSilenceForChannel(), TestShouldSilenceForChannel(), TestParseChatID(), TestConnectParamsRoundTrip(), TestHelloOkRoundTrip(), TestValidateConnectParams(), TestValidateProtocolVersion(), ValidateConnectParams() (+175 more)

### Community 7 - "Telegram"
Cohesion: 0.02
Nodes (128): TestBackoff_Delay(), TestBackoff_Jitter(), NewChannelCallbacks(), CallbackSnapshot, ChannelCallbacks, ParseChatID(), New(), retryAfterFromParams() (+120 more)

### Community 8 - "Chat"
Cohesion: 0.02
Nodes (152): ACPDelivery, ACPDispatch, ACPDispatchConfig, ACPEventOutput, ACPPromptInput, ACPResource, ACPTranslator, IsACPSession() (+144 more)

### Community 9 - "Config"
Cohesion: 0.02
Nodes (177): FileCacheEntry, copyFile(), Options, TestWriteFile_Backup(), TestWriteFile_Basic(), TestWriteFile_ConcurrentSafety(), TestWriteFile_CreatesParentDirs(), TestWriteFile_NoLeftoverTempFiles() (+169 more)

### Community 10 - "Tools"
Cohesion: 0.02
Nodes (164): SlashResult, appendDuration(), appendTimestamp(), appendTwoDigits(), appendValue(), levelBarStyle(), levelText(), needsQuote() (+156 more)

### Community 11 - "Skills"
Cohesion: 0.02
Nodes (172): TestAgentSpawnRequestJSON(), TestAgentStatusIsTerminal(), AnalyzeEmail(), FormatEmailForAnalysis(), TestResolveSkillInvocationPolicy(), modelResolution, IsValidSkillType(), normalizeSafeBrewFormula() (+164 more)

### Community 12 - "Dev"
Cohesion: 0.02
Nodes (159): Request, _call_anthropic(), _call_judge(), _call_openai_compat(), judge_absolute(), judge_absolute_score(), judge_available(), judge_pairwise() (+151 more)

### Community 13 - "Rpc"
Cohesion: 0.02
Nodes (146): fakeLLMStreamer, ReplyDeps, Chain(), Logging(), okHandler(), TestChain_Order(), TestLogging_Middleware(), RegisterACPMethods() (+138 more)

### Community 14 - "Chat"
Cohesion: 0.02
Nodes (129): toolCallRecord, ToolLoopConfig, ToolLoopDetector, ToolLoopLevel, ToolLoopResult, AgentErrorKind, agentConfigDeps, chatportAdapters (+121 more)

### Community 15 - "Handlers"
Cohesion: 0.02
Nodes (122): ApplyAbortCutoffToSessionEntry(), ClearAbortCutoffInSession(), HasAbortCutoff(), isFiniteInt64(), ReadAbortCutoffFromSessionEntry(), ShouldPersistAbortCutoff(), ShouldSkipMessageByAbortCutoff(), TestAbortCutoffLifecycle() (+114 more)

### Community 16 - "Cron"
Cohesion: 0.03
Nodes (104): TruncateLine(), DeliverOutputOptions, DeliveryResult, DeliveryTarget, JobDeliveryConfig, Service, SmartScheduleOpts, Store (+96 more)

### Community 17 - "Acp"
Cohesion: 0.03
Nodes (92): ACPAgent, ACPProjector, ACPRegistry, ACPTokenUsage, ACPTurnResult, AgentBindingEntry, BindingStore, BindingStoreFile (+84 more)

### Community 18 - "Provider"
Cohesion: 0.02
Nodes (83): credKey(), NewAuthManager(), TestAuthManager_Prepare_NoForwarder(), TestAuthManager_StoreResolve(), TestManagedCredential_Expiry(), NormalizeProviderID(), NormalizeProviderIDForAuth(), ResolveDiscoveryProviders() (+75 more)

### Community 19 - "Polaris"
Cohesion: 0.03
Nodes (74): assembleContextFull(), chatToLLM(), selectBestSummaries(), sortByMsgStart(), TestAssembleContextFull_EmptyStore(), TestAssembleContextFull_MultiLevelSummaries(), TestAssembleContextFull_RecentOnly(), TestAssembleContextFull_TokenBudgetTrimsOldestSummaries() (+66 more)

### Community 20 - "Events"
Cohesion: 0.03
Nodes (62): NewBroadcaster(), TestBroadcast_AllSubscribers(), TestBroadcast_Filter(), TestBroadcast_SendError(), TestBroadcast_SkipsUnauthenticated(), TestBroadcastRaw(), TestBroadcastToConnIDs(), TestSequenceIncrement() (+54 more)

### Community 21 - "Autonomous"
Cohesion: 0.03
Nodes (55): spillEntry, SpilloverStore, CycleEvent, errorNotifier, EventListener, fakeDreamer, fakeNotifier, fakeTask (+47 more)

### Community 22 - "Compaction"
Cohesion: 0.04
Nodes (102): contextCapturingToolExecutor, fakeToolExecutor, toolCall, toolUseSpec, turnResult, trimLLMToTokenBudget(), BootstrapCompact(), Config (+94 more)

### Community 23 - "Process"
Cohesion: 0.03
Nodes (67): formatUptime(), pick(), PrintBanner(), PrintShutdown(), TestFormatUptime(), LoggingResult, Run(), Services (+59 more)

### Community 24 - "Web"
Cohesion: 0.04
Nodes (93): countMediaEntries(), FinalizeInboundContextFull(), firstNonEmpty(), normalizeChatType(), normalizeMediaType(), normalizeOptionalTextField(), sanitizeInboundText(), stripSystemTags() (+85 more)

### Community 25 - "Toolreg"
Cohesion: 0.04
Nodes (72): ToolRegistry, DeferredActivationFromContext(), FetchToolsSchema(), RegisterChronoTools(), RegisterCoreTools(), RegisterFSTools(), RegisterMediaTools(), RegisterPolarisTools() (+64 more)

### Community 26 - "Gmail"
Cohesion: 0.05
Nodes (42): credentialsDir(), DefaultClient(), NewClient(), setTokenURL(), TestGetClient_RetriableOnFailure(), truncate(), clampGmailMax(), Client (+34 more)

### Community 27 - "Typing"
Cohesion: 0.05
Nodes (32): BaseModel, embed(), EmbedRequest, EmbedResponse, load_model(), main(), Sync handler — FastAPI runs it in a threadpool, keeping the event loop free., Load BGE-M3 GGUF model via llama-cpp-python. (+24 more)

### Community 28 - "Prompt"
Cohesion: 0.05
Nodes (44): ClearSessionSnapshot(), collectSearchDirs(), FormatContextFilesForPrompt(), LoadContextFiles(), loadContextFilesFromDisk(), ResetContextFileCacheForTest(), TestFormatContextFilesForPrompt(), TestLoadContextFiles() (+36 more)

### Community 29 - "Subagent"
Cohesion: 0.06
Nodes (57): formatRelativeAge(), FormatTimestampWithAge(), HandleSubagentsAgentsAction(), HandleSubagentsFocusAction(), HandleSubagentsUnfocusAction(), HandleSubagentsLogAction(), HandleSubagentsSendAction(), HandleSubagentsSpawnAction() (+49 more)

### Community 30 - "Handlertask"
Cohesion: 0.11
Nodes (52): CompleteTask(), TestDaemonStatus_running(), TestHealth_returnsOK(), TestRuntimeMethods_registersAllHandlers(), TestStatus_withDeps(), TestSystemEvent_withBroadcast(), TestSystemPresence_invalidParams(), TestSystemPresence_withBroadcast() (+44 more)

### Community 31 - "Claude"
Cohesion: 0.05
Nodes (51): Core Skills Roster, AGENTS First Run, Memory System (daily + MEMORY.md), AGENTS Safety Defaults, AGENTS Session Start Protocol, Single-user Infrastructure Cleanup, Design Principles (opinionated defaults), Korean Language First (+43 more)

### Community 32 - "Session"
Cohesion: 0.08
Nodes (25): ChannelHealthConfig, ChannelHealthDeps, ChannelHealthMonitor, ChannelHealthResult, TransitionError, BenchmarkIsTerminal(), BenchmarkIsValidTransition(), BenchmarkValidateTransition() (+17 more)

### Community 33 - "Skills"
Cohesion: 0.07
Nodes (34): Conditional Activation (requires_tools/fallback_for_tools), hermes-agent (NousResearch) Citation, Progressive Loading (3-stage), Skills Prompt Cache Design (semi-static block), Nested Category Layout, SKILL.md Frontmatter Standard, Hermes-Agent Skill Lifecycle, Skill vs Tool Decision Framework (+26 more)

### Community 34 - "Session"
Cohesion: 0.13
Nodes (31): cloneInt64Ptr(), DeriveLifecycleSnapshot(), isFiniteTimestamp(), resolveLifecycleEndedAt(), resolveLifecyclePhase(), resolveLifecycleStartedAt(), resolveRuntimeMs(), resolveTerminalStatus() (+23 more)

### Community 35 - "Agent"
Cohesion: 0.15
Nodes (14): JobTracker, LifecycleEvent, pendingError, RunSnapshot, RunStatus, NewJobTracker(), TestJobTracker_AbortedRunTimeout(), TestJobTracker_ActiveRunCount() (+6 more)

### Community 36 - "Config"
Cohesion: 0.08
Nodes (25): AgentEntryConfig, AgentsConfig, AgentsDefaultsConfig, ChannelsConfig, CronConfig, DenebConfig, GatewayAuthConfig, GatewayAuthRateLimitConfig (+17 more)

### Community 37 - "Telegram"
Cohesion: 0.08
Nodes (24): Animation, APIResponse, Audio, CallbackQuery, Chat, Document, File, ForumTopic (+16 more)

### Community 38 - "Provider"
Cohesion: 0.15
Nodes (14): BuildPairedProviderAPIKeyCatalog(), BuildSingleProviderAPIKeyCatalog(), FindCatalogTemplate(), TestBuildPairedProviderAPIKeyCatalog(), TestBuildSingleProviderAPIKeyCatalog(), TestBuildSingleProviderAPIKeyCatalogWithExplicitBaseURL(), TestFindCatalogTemplate(), CatalogBuilderContext (+6 more)

### Community 39 - "Llm"
Cohesion: 0.14
Nodes (13): completionTokensDetails, openAIChunk, openAIContentPart, openAIDelta, openAIDeltaToolCall, openAIFunction, openAIImgURL, openAIMessage (+5 more)

### Community 40 - "Httpretry"
Cohesion: 0.2
Nodes (6): APIError, Category, Classify(), IsRetryable(), TestClassify(), TestIsRetryable()

### Community 41 - "Cron"
Cohesion: 0.17
Nodes (11): AgentRunner, AgentTurnParams, CronEvent, CronEventListener, ListOptions, ListPageOptions, ListPageResult, RunOutcome (+3 more)

### Community 42 - "Types"
Cohesion: 0.18
Nodes (10): AgentRunStartParams, BlockReplyContext, CommandControl, GetReplyOptions, MediaContext, ModelSelectedContext, MsgContext, SenderInfo (+2 more)

### Community 43 - "Wiki"
Cohesion: 0.31
Nodes (8): ConfigFromEnv(), envBool(), envFloat(), envInt(), envStr(), ResetConfigForTest(), TestConfigFromEnv_Overrides(), Config

### Community 44 - "Agentlog"
Cohesion: 0.25
Nodes (7): LogEntry, RunEndData, RunErrorData, RunPrepData, RunStartData, TurnLLMData, TurnToolData

### Community 45 - "Provider"
Cohesion: 0.25
Nodes (7): AuthMethod, Capabilities, CatalogContext, CatalogEntry, CatalogResult, PreparedAuth, RuntimeAuthContext

### Community 46 - "Healthcheck"
Cohesion: 0.25
Nodes (8): DevOps Category Description, healthcheck Skill (Host Hardening), Deneb CLI Commands (security audit, update status, cron), Rationale: Treat OS hardening as separate from Deneb tooling, Risk Profiles (Home Balanced, VPS Hardened, Developer Convenience, Custom), tmux Skill (Session Control), Claude Code Session Patterns (approve prompts), Rationale: Split text+Enter sends to avoid paste edge cases

### Community 47 - "Types"
Cohesion: 0.29
Nodes (6): BuildReplyPayloadsParams, DeliverFunc, MessagingToolTarget, ReplyDispatchKind, ReplyPayload, TypingPolicy

### Community 48 - "Toolctx"
Cohesion: 0.33
Nodes (5): ChronoDeps, CoreToolDeps, ProcessDeps, SessionDeps, WikiDeps

### Community 49 - "Chatport"
Cohesion: 0.33
Nodes (5): DraftSanitizerFunc, IsTransientErrorFunc, ParseReplyDirectivesFunc, ReplyDirectives, TypingSignaler

### Community 50 - "Polaris"
Cohesion: 0.33
Nodes (3): CompactUrgency, Config, SummaryNode

### Community 51 - "Agent"
Cohesion: 0.4
Nodes (4): AgentConfig, AgentResult, ToolActivity, TurnCallback

### Community 52 - "Protocol"
Cohesion: 0.4
Nodes (4): ProviderAuthMethod, ProviderCatalogEntry, ProviderCatalogSnapshot, ProviderMeta

### Community 53 - "Claude"
Cohesion: 0.5
Nodes (5): Adding New RPC Method Procedure, Adding New Agent Tool Procedure, Gateway Directory Map, Generated Files Boundary, Go Gateway Module Overview

### Community 54 - "Autoreply"
Cohesion: 0.5
Nodes (3): AgentExecutor, AgentTurnConfig, AgentTurnResult

### Community 55 - "Changelog"
Cohesion: 0.5
Nodes (4): Memory Token Budget 150k, Polaris Compaction System, Replace Perplexity with Tavily, Conventional Commit Format

### Community 56 - "Tasks"
Cohesion: 0.5
Nodes (4): Background Task Control Plane Architecture, Task Runtime/Status/Flow Concepts, Adding Runtime Integration, Task/Flow RPC Methods

### Community 57 - "Testdata"
Cohesion: 0.5
Nodes (4): Filler Fixture: Database, Filler Fixture: Distributed Systems, Filler Fixture: Kubernetes, Filler Fixture: Streaming

### Community 58 - "Chat"
Cohesion: 0.67
Nodes (2): SyncOptions, SyncResult

### Community 59 - "Autonomous"
Cohesion: 0.67
Nodes (2): PeriodicTask, TaskStatus

### Community 60 - "Autonomous"
Cohesion: 0.67
Nodes (2): Dreamer, DreamReport

### Community 61 - "Cron"
Cohesion: 0.67
Nodes (2): CronFailureAlert, CronSessionTarget

### Community 62 - "Claude"
Cohesion: 0.67
Nodes (3): Context Engineering Policy, Rules Index Table, Rationale: Conditional rules prevent context bloat

### Community 63 - "Tools"
Cohesion: 0.67
Nodes (3): ClawHub (shared skills), Creating Custom Skills Guide, SKILL.md Format (YAML + Markdown)

### Community 64 - "Agent"
Cohesion: 1.0
Nodes (1): LLMStreamer

### Community 65 - "Agent"
Cohesion: 1.0
Nodes (1): ToolExecutor

### Community 66 - "Server"
Cohesion: 1.0
Nodes (1): HookManager

### Community 67 - "Server"
Cohesion: 1.0
Nodes (1): MemorySubsystem

### Community 68 - "Server"
Cohesion: 1.0
Nodes (1): GenesisSubsystem

### Community 69 - "Server"
Cohesion: 1.0
Nodes (1): AutonomousSubsystem

### Community 70 - "Server"
Cohesion: 1.0
Nodes (1): SessionManager

### Community 71 - "Server"
Cohesion: 1.0
Nodes (1): ChatManager

### Community 72 - "Claude"
Cohesion: 1.0
Nodes (2): Build Hard Gates (make check), Live Testing Hard Gate

### Community 73 - "Scripts"
Cohesion: 1.0
Nodes (2): Codespell Dictionary Wordlist, Codespell Ignore Wordlist

### Community 74 - "Chat"
Cohesion: 1.0
Nodes (0): 

### Community 75 - "Chat"
Cohesion: 1.0
Nodes (0): 

### Community 76 - "Chat"
Cohesion: 1.0
Nodes (0): 

### Community 77 - "Tools"
Cohesion: 1.0
Nodes (0): 

### Community 78 - "Timeouts"
Cohesion: 1.0
Nodes (0): 

### Community 79 - "Cron"
Cohesion: 1.0
Nodes (0): 

### Community 80 - "Cron"
Cohesion: 1.0
Nodes (0): 

### Community 81 - "Cron"
Cohesion: 1.0
Nodes (0): 

### Community 82 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 83 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 84 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 85 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 86 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 87 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 88 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 89 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 90 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 91 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 92 - "Server"
Cohesion: 1.0
Nodes (0): 

### Community 93 - "Rpc"
Cohesion: 1.0
Nodes (0): 

### Community 94 - "System"
Cohesion: 1.0
Nodes (0): 

### Community 95 - "Process"
Cohesion: 1.0
Nodes (0): 

### Community 96 - "Process"
Cohesion: 1.0
Nodes (0): 

### Community 97 - "Process"
Cohesion: 1.0
Nodes (0): 

### Community 98 - "Genesis"
Cohesion: 1.0
Nodes (0): 

### Community 99 - "Protocol"
Cohesion: 1.0
Nodes (0): 

### Community 100 - "Protocol"
Cohesion: 1.0
Nodes (0): 

### Community 101 - "Changelog"
Cohesion: 1.0
Nodes (1): RLM Observation Traces

### Community 102 - "Changelog"
Cohesion: 1.0
Nodes (1): Wiki Karpathy Concept

### Community 103 - "Changelog"
Cohesion: 1.0
Nodes (1): Bind[P] Generics Refactor

### Community 104 - "Readme"
Cohesion: 1.0
Nodes (1): Prerequisites (Go 1.24+, buf, DGX Spark)

### Community 105 - "Testdata"
Cohesion: 1.0
Nodes (1): Filler Fixture: Security

### Community 106 - "Testdata"
Cohesion: 1.0
Nodes (1): Filler Fixture: ML Infrastructure

### Community 107 - "Testdata"
Cohesion: 1.0
Nodes (1): Filler Fixture: Observability

### Community 108 - "Integration"
Cohesion: 1.0
Nodes (1): Integration Category Description

## Knowledge Gaps
- **836 isolated node(s):** `SyncResult`, `SyncOptions`, `HandlerConfig`, `StatusDepsFunc`, `StatusDeps` (+831 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `Agent`** (2 nodes): `LLMStreamer`, `client.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Agent`** (2 nodes): `ToolExecutor`, `tool.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `hook_manager.go`, `HookManager`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `memory_subsystem.go`, `MemorySubsystem`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `genesis_subsystem.go`, `GenesisSubsystem`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `autonomous_subsystem.go`, `AutonomousSubsystem`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `session_manager.go`, `SessionManager`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (2 nodes): `chat_manager.go`, `ChatManager`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Claude`** (2 nodes): `Build Hard Gates (make check)`, `Live Testing Hard Gate`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Scripts`** (2 nodes): `Codespell Dictionary Wordlist`, `Codespell Ignore Wordlist`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Chat`** (1 nodes): `types.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Chat`** (1 nodes): `tool_classification_gen.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Chat`** (1 nodes): `tools_deps.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Tools`** (1 nodes): `types.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Timeouts`** (1 nodes): `timeouts.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Cron`** (1 nodes): `service_scheduler.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Cron`** (1 nodes): `service_lifecycle.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Cron`** (1 nodes): `service_events.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_rpc_session.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_rpc_auth.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `init_genesis.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `method_registry.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_rpc_channel.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `chat_pipeline.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `session_restore.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_init_acp.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_http_routing.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `gateway_hub.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Server`** (1 nodes): `server_monitoring.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Rpc`** (1 nodes): `register.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `System`** (1 nodes): `system.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Process`** (1 nodes): `process_test.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Process`** (1 nodes): `process.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Process`** (1 nodes): `env_blocklist_gen.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Genesis`** (1 nodes): `prompts.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Protocol`** (1 nodes): `constants.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Protocol`** (1 nodes): `errors.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Changelog`** (1 nodes): `RLM Observation Traces`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Changelog`** (1 nodes): `Wiki Karpathy Concept`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Changelog`** (1 nodes): `Bind[P] Generics Refactor`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Readme`** (1 nodes): `Prerequisites (Go 1.24+, buf, DGX Spark)`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Testdata`** (1 nodes): `Filler Fixture: Security`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Testdata`** (1 nodes): `Filler Fixture: ML Infrastructure`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Testdata`** (1 nodes): `Filler Fixture: Observability`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Integration`** (1 nodes): `Integration Category Description`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Append()` connect `Chat` to `Chat`, `Htmlmd`, `Coremarkdown`, `Skills`, `Wiki`, `Tasks`, `Session`, `Telegram`, `Config`, `Tools`, `Skills`, `Dev`, `Rpc`, `Chat`, `Handlers`, `Cron`, `Acp`, `Provider`, `Polaris`, `Events`, `Autonomous`, `Compaction`, `Process`, `Web`, `Toolreg`, `Gmail`, `Typing`, `Prompt`, `Subagent`, `Session`?**
  _High betweenness centrality (0.146) - this node is a cross-community bridge._
- **Why does `run()` connect `Session` to `Chat`, `Htmlmd`, `Coremarkdown`, `Skills`, `Wiki`, `Tasks`, `Telegram`, `Chat`, `Config`, `Tools`, `Skills`, `Dev`, `Rpc`, `Chat`, `Handlers`, `Cron`, `Acp`, `Provider`, `Events`, `Autonomous`, `Process`, `Web`, `Toolreg`, `Typing`, `Prompt`, `Session`, `Session`?**
  _High betweenness centrality (0.058) - this node is a cross-community bridge._
- **Why does `contains()` connect `Htmlmd` to `Chat`, `Coremarkdown`, `Skills`, `Wiki`, `Tasks`, `Session`, `Telegram`, `Chat`, `Config`, `Tools`, `Skills`, `Rpc`, `Chat`, `Handlers`, `Cron`, `Acp`, `Polaris`, `Autonomous`, `Compaction`, `Process`, `Web`, `Toolreg`, `Gmail`, `Prompt`, `Subagent`?**
  _High betweenness centrality (0.038) - this node is a cross-community bridge._
- **Are the 483 inferred relationships involving `Append()` (e.g. with `generate()` and `sortedKeys()`) actually correct?**
  _`Append()` has 483 INFERRED edges - model-reasoned connections that need verification._
- **Are the 281 inferred relationships involving `contains()` (e.g. with `TestOutputTrimmer_Long()` and `TestErrorEnricher_PermissionDenied()`) actually correct?**
  _`contains()` has 281 INFERRED edges - model-reasoned connections that need verification._
- **Are the 202 inferred relationships involving `Unmarshal()` (e.g. with `main()` and `extractFilePath()`) actually correct?**
  _`Unmarshal()` has 202 INFERRED edges - model-reasoned connections that need verification._
- **Are the 173 inferred relationships involving `run()` (e.g. with `main()` and `BenchmarkPreSerialize_vs_RawMarshal()`) actually correct?**
  _`run()` has 173 INFERRED edges - model-reasoned connections that need verification._