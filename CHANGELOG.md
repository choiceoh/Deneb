# Changelog

## [3.13.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.12.0...deneb-v3.13.0) (2026-03-27)

### ✨ Features

* add LiteParse integration for document parsing ([9b91fda](https://github.com/choiceoh/Deneb/commit/9b91fda))
* replace simple typing ticker with phase-aware FullTypingSignaler ([def5a92](https://github.com/choiceoh/Deneb/commit/def5a92))
* add status reaction emoji on user message during agent runs ([3351488](https://github.com/choiceoh/Deneb/commit/3351488))
* add cargo-deny config, DuckDB analytics scripts, and Makefile targets ([9793986c25b600ad38ebcf1a2b22ca9cafb26b5d](https://github.com/choiceoh/Deneb/commit/9793986c25b600ad38ebcf1a2b22ca9cafb26b5d))
* **go:** add property-based and benchmark tests for session and RPC ([bee0d077e6f7b8d0f89e865c010f8cc90308480d](https://github.com/choiceoh/Deneb/commit/bee0d077e6f7b8d0f89e865c010f8cc90308480d))
* **go:** add stdlib metrics package with Prometheus-compatible /metrics endpoint ([dd9861d4de54628e8da1763a275219a0e31dd280](https://github.com/choiceoh/Deneb/commit/dd9861d4de54628e8da1763a275219a0e31dd280))
* **rust:** add proptest property tests for protocol frames and cosine similarity ([22612028307ed571158339519fd764e4f509c316](https://github.com/choiceoh/Deneb/commit/22612028307ed571158339519fd764e4f509c316))

### 🐛 Bug Fixes

* fix status reactions with Telegram-compatible emojis and error logging ([41a5099](https://github.com/choiceoh/Deneb/commit/41a5099))

## [3.12.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.7...deneb-v3.12.0) (2026-03-27)

### ✨ Features

* auto-detect embedding server on DGX Spark startup ([affd107512bea1ad75993eff3bf0558045992ba0](https://github.com/choiceoh/Deneb/commit/affd107512bea1ad75993eff3bf0558045992ba0))
* auto-launch SGLang embedding server on DGX Spark ([08620fe4641780790eb2d4a9120a50a48439941a](https://github.com/choiceoh/Deneb/commit/08620fe4641780790eb2d4a9120a50a48439941a))

### 🐛 Bug Fixes

* separate embedding model from chat model to prevent SGLang 400 errors ([c0c2a0126263556b39639c8bfbd3ee98c727a86d](https://github.com/choiceoh/Deneb/commit/c0c2a0126263556b39639c8bfbd3ee98c727a86d))
* separate embedding model from chat model, auto-launch on DGX Spark ([a626c70a5f30ebe2d7a7c0a9eeeeac2b14a9bb0d](https://github.com/choiceoh/Deneb/commit/a626c70a5f30ebe2d7a7c0a9eeeeac2b14a9bb0d))

## [3.11.7](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.6...deneb-v3.11.7) (2026-03-26)

### ✨ Features

* enhance inter-tool integration ([08c061f](https://github.com/choiceoh/Deneb/commit/08c061f))
* enrich tool descriptions in system prompt ([47f70c7](https://github.com/choiceoh/Deneb/commit/47f70c7))
* replace GGUF models with SGLang for embedding and query expansion ([a15721e](https://github.com/choiceoh/Deneb/commit/a15721e))
* add gmail tool with inbox summary, search, send, reply, labels and contact aliases ([4cf9158](https://github.com/choiceoh/Deneb/commit/4cf9158))
* add gmail tool usage guide to system prompt tool selection ([39c24d2](https://github.com/choiceoh/Deneb/commit/39c24d2))
* add Honcho-inspired structured memory system with SGLang ([8b37aa2](https://github.com/choiceoh/Deneb/commit/8b37aa2))
* deep improvements — gateway init, dedup, migration, conflict resolution, Korean FTS, mid-run extraction, Neuromancer-style prompts ([fb317cd](https://github.com/choiceoh/Deneb/commit/fb317cd))
* wire StartPeriodicTimer at gateway init ([0b9aad0](https://github.com/choiceoh/Deneb/commit/0b9aad0))

### ⚡ Performance

* optimize inbound pipeline latency (async handler, reduced timeouts) ([f0c6d3c](https://github.com/choiceoh/Deneb/commit/f0c6d3c))
* improve cache utilization across hot paths ([54e1d4a](https://github.com/choiceoh/Deneb/commit/54e1d4a))
* parallelize knowledge+context prep, add pipeline timing, reduce proactive timeout ([7cfec95](https://github.com/choiceoh/Deneb/commit/7cfec95))
* reduce compress timeout (30s→10s) and raise threshold (8K→16K) ([e27790d](https://github.com/choiceoh/Deneb/commit/e27790d))

### 🐛 Bug Fixes

* resolve all failing tests across Rust core and Go gateway ([6dc20d9677438868800493f3b55bca95df12030c](https://github.com/choiceoh/Deneb/commit/6dc20d9677438868800493f3b55bca95df12030c))
* fix code review issues ([9d7804d](https://github.com/choiceoh/Deneb/commit/9d7804d))
* fix deadlock, race condition, and deduplicate stream helpers ([276146c](https://github.com/choiceoh/Deneb/commit/276146c))

## [3.11.6](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.5...deneb-v3.11.6) (2026-03-26)

### ✨ Features

* include workspace directory in sglang system prompt ([046cd6d](https://github.com/choiceoh/Deneb/commit/046cd6d))
* add .env file loading and upgrade Perplexity to sonar-reasoning-pro ([664af90](https://github.com/choiceoh/Deneb/commit/664af90))
* Add sessions_search and sessions_restore tools for transcript management ([545b45e](https://github.com/choiceoh/Deneb/commit/545b45e))
* add health check, thinking mode, chaining, smart truncation, and metrics ([75061a7](https://github.com/choiceoh/Deneb/commit/75061a7))
* add JSON response cleaning for output_format json ([6bd62be](https://github.com/choiceoh/Deneb/commit/6bd62be))
* integrate Vega + Memory knowledge prefetch into context assembly ([b531601](https://github.com/choiceoh/Deneb/commit/b531601))
* improve agent tool usage, response speed, and action efficiency ([e82df17](https://github.com/choiceoh/Deneb/commit/e82df17))
* add output post-processing — markdown normalization, list cleaning, length enforcement ([5d6b14c](https://github.com/choiceoh/Deneb/commit/5d6b14c))

### 🐛 Bug Fixes

* add missing errors import in ml_cgo.go ([599d403](https://github.com/choiceoh/Deneb/commit/599d403))
* fix truncation overlap panic, brief+thinking conflict, chain self-call guard ([35f7f8e](https://github.com/choiceoh/Deneb/commit/35f7f8e))
* fix review issues — UTF-8 truncation, Anthropic path, short message filter ([d0014f2](https://github.com/choiceoh/Deneb/commit/d0014f2))
* use MarkdownToTelegramHTML for reply formatting (fixes fenced code blocks) ([5db3d35](https://github.com/choiceoh/Deneb/commit/5db3d35))

### 🔧 Internal

* split oversized commands_handlers.go and parser.rs into focused domain files ([d1aa584899654f2469d584a832c7698d5305b836](https://github.com/choiceoh/Deneb/commit/d1aa584899654f2469d584a832c7698d5305b836))
* modernize console handler with decisecond timestamps, pkg tags, separator, and human-friendly durations ([a983420](https://github.com/choiceoh/Deneb/commit/a983420))

## [3.11.5](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.4...deneb-v3.11.5) (2026-03-26)

### ✨ Features

* graceful SIGTERM before SIGKILL and process group isolation ([86a66b8](https://github.com/choiceoh/Deneb/commit/86a66b8))
* add link understanding and interactive replies ([b550469](https://github.com/choiceoh/Deneb/commit/b550469))
* implement subagents tool with session manager integration ([ea085d2](https://github.com/choiceoh/Deneb/commit/ea085d2))
* implement full ACP RPC subsystem ([dda6764](https://github.com/choiceoh/Deneb/commit/dda6764))
* Port channel utilities from TypeScript to Go ([68b9806](https://github.com/choiceoh/Deneb/commit/68b9806))
* Add link understanding and reply context for Telegram messages ([5bfe66a](https://github.com/choiceoh/Deneb/commit/5bfe66a))
* Add media extraction: YouTube transcripts, video frames, and Telegram attachments ([efd5dbd](https://github.com/choiceoh/Deneb/commit/efd5dbd))
* Add human-readable console log handler with optional color output ([b672520](https://github.com/choiceoh/Deneb/commit/b672520))
* Add autonomous goal-driven execution system with attention-based triggering ([4db1867](https://github.com/choiceoh/Deneb/commit/4db1867))
* Add goal starvation detection, note history, and stale goal auto-pause ([ab82843](https://github.com/choiceoh/Deneb/commit/ab82843))
* enhance /status with server-level system info ([b31e28a](https://github.com/choiceoh/Deneb/commit/b31e28a))
* deepen TS→Go port: ChannelsConfig types, cron run log wiring, wizard multi-step ([73a0ebd](https://github.com/choiceoh/Deneb/commit/73a0ebd))
* Increase context limits and adjust chat configuration defaults ([2a519db](https://github.com/choiceoh/Deneb/commit/2a519db))
* add sglang fallback and local summarization ([9b48bf8](https://github.com/choiceoh/Deneb/commit/9b48bf8))
* add send_file, http, kv, clipboard agent tools ([37c6645](https://github.com/choiceoh/Deneb/commit/37c6645))
* optimize AI agent tools for parallel execution and richer schemas ([b616bd6](https://github.com/choiceoh/Deneb/commit/b616bd6))
* enhance tool schemas with enum/default constraints ([99e9150](https://github.com/choiceoh/Deneb/commit/99e9150))
* Unified web tool: search, fetch, and search+fetch in one ([6a34382](https://github.com/choiceoh/Deneb/commit/6a34382))
* Add Pilot tool and Copilot background monitor with local sglang ([78a33fb](https://github.com/choiceoh/Deneb/commit/78a33fb))

### ⚡ Performance

* DGX Spark 20-core CPU utilization + chat latency optimization ([405](https://github.com/choiceoh/Deneb/issues/405)) ([1162f35f5372dc0ad0ca619afabea1be66b0747b](https://github.com/choiceoh/Deneb/commit/1162f35f5372dc0ad0ca619afabea1be66b0747b))
* bypass RPC for model prewarm, call LLM directly ([7696cfe](https://github.com/choiceoh/Deneb/commit/7696cfe))
* add mtime-based context file caching, TTL memory file list cache, Anthropic prompt cache_control breakpoints ([dbec372](https://github.com/choiceoh/Deneb/commit/dbec372))
* Optimize for DGX Spark: SIMD, parallelism, and caching ([957aa1b](https://github.com/choiceoh/Deneb/commit/957aa1b))

### 🐛 Bug Fixes

* fix signal killed by OOM and graceful shutdown ([6da475a](https://github.com/choiceoh/Deneb/commit/6da475a))
* remove OOM score adjustment logic ([64ae762](https://github.com/choiceoh/Deneb/commit/64ae762))
* fix clippy errors, Go formatting, proto generation ([8e75340](https://github.com/choiceoh/Deneb/commit/8e75340))
* autonomous: fix bugs, deadlock risks, and add comprehensive tests ([1a66bda](https://github.com/choiceoh/Deneb/commit/1a66bda))
* core-rs: harden markdown parser and parameterize SQL ID lists ([2ce5ec2](https://github.com/choiceoh/Deneb/commit/2ce5ec2))
* autonomous: fix production issues — transcript reset, error tracking, enabled persistence ([dba1f21](https://github.com/choiceoh/Deneb/commit/dba1f21))
* watchdog: skip stale-activity restart when autonomous service is running ([8c44fd6](https://github.com/choiceoh/Deneb/commit/8c44fd6))
* compaction: fix SystemPromptAddition loss, improve summary quality ([c625798](https://github.com/choiceoh/Deneb/commit/c625798))

### 🔧 Internal

* Improve code clarity: add mutex guards and simplify error handling ([b8d0ce6](https://github.com/choiceoh/Deneb/commit/b8d0ce6))
* core-rs: add SAFETY comments, fix Mutex unwrap, add module docs ([71e49fb](https://github.com/choiceoh/Deneb/commit/71e49fb))
* cleanup: remove unused folders and files ([722e881](https://github.com/choiceoh/Deneb/commit/722e881))
* Refactor RPC handlers into domain-based subpackages ([75063a7](https://github.com/choiceoh/Deneb/commit/75063a7))

## [3.11.4](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.3...deneb-v3.11.4) (2026-03-26)

### ✨ Features

* add SOUL.md activation instruction to system prompt ([5284377](https://github.com/choiceoh/Deneb/commit/5284377))
* implement Go host-side orchestration for Rust compaction engine ([8406e00](https://github.com/choiceoh/Deneb/commit/8406e00))
* implement stub tools and ACP wiring ([6d375cd](https://github.com/choiceoh/Deneb/commit/6d375cd))

### 🐛 Bug Fixes

* correct FFI error codes for session key validation and buffer-too-small returns ([352](https://github.com/choiceoh/Deneb/issues/352)) ([8f882ccaffee5b0a8e722bf7146b3f3d326750bc](https://github.com/choiceoh/Deneb/commit/8f882ccaffee5b0a8e722bf7146b3f3d326750bc))
* downgrade context canceled polling error to info level ([90bba33](https://github.com/choiceoh/Deneb/commit/90bba33))
* fix WebSocket connectivity, session GC, and channel restart reliability ([6eab080](https://github.com/choiceoh/Deneb/commit/6eab080))

## [3.11.3](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.2...deneb-v3.11.3) (2026-03-26)

### ✨ Features

* wire agent cron tool to actual scheduler instead of stubs ([0c0334c](https://github.com/choiceoh/Deneb/commit/0c0334c))
* wire disconnected packages to RPC/server layer ([43ee8fb](https://github.com/choiceoh/Deneb/commit/43ee8fb))

### 🐛 Bug Fixes

* strip Telegram @bot suffix from slash commands ([344](https://github.com/choiceoh/Deneb/issues/344)) ([2b94c45246d37a9adb8079ae3d7766aa972ac11e](https://github.com/choiceoh/Deneb/commit/2b94c45246d37a9adb8079ae3d7766aa972ac11e))

### 🔧 Internal

* remove AGENTS.md symlink, make CLAUDE.md the real file ([44fb8a9](https://github.com/choiceoh/Deneb/commit/44fb8a9))
* remove JS/TS/Python dependencies and all references ([4da9110](https://github.com/choiceoh/Deneb/commit/4da9110))

## [3.11.2](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.1...deneb-v3.11.2) (2026-03-26)

### 🐛 Bug Fixes

* handle agents.defaults.model as json.RawMessage (string or object) ([339](https://github.com/choiceoh/Deneb/issues/339)) ([ad11af9fcd58abe4f175034020034cc0b90d3a91](https://github.com/choiceoh/Deneb/commit/ad11af9fcd58abe4f175034020034cc0b90d3a91))
* fix: agents.defaults.model parsing + memory_search diagnostics ([c08f09e](https://github.com/choiceoh/Deneb/commit/c08f09e))

### 🔧 Internal

* Go/Rust 마이그레이션 평가 및 잔여 격차 해소 ([8a44a86](https://github.com/choiceoh/Deneb/commit/8a44a86))

## [3.11.1](https://github.com/choiceoh/Deneb/compare/deneb-v3.11.0...deneb-v3.11.1) (2026-03-26)

### 🐛 Bug Fixes

* resolve autoreply duplicate declarations and model config parsing ([334](https://github.com/choiceoh/Deneb/issues/334)) ([f7737bdc0b157a24db2fc1f588ca4d34f185e8d6](https://github.com/choiceoh/Deneb/commit/f7737bdc0b157a24db2fc1f588ca4d34f185e8d6))
* resolve Go gateway workspace dir from config instead of os.Getwd() ([337](https://github.com/choiceoh/Deneb/issues/337)) ([ae6b9a06a67b401868df151bc3699a7e109d1c9f](https://github.com/choiceoh/Deneb/commit/ae6b9a06a67b401868df151bc3699a7e109d1c9f))

### 🔧 Internal

* Remove TypeScript codebase entirely ([50aba9c](https://github.com/choiceoh/Deneb/commit/50aba9c))

## [3.11.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.10.0...deneb-v3.11.0) (2026-03-26)


### Features

* complete Python-to-Rust migration for Vega ([#304](https://github.com/choiceoh/Deneb/issues/304)) ([e93e541](https://github.com/choiceoh/Deneb/commit/e93e541fadb2d58e5d6dca58415156f425be2bc4))


### Bug Fixes

* correct Rust base64 test assertion, Go ML test stub handling, and format drift ([#316](https://github.com/choiceoh/Deneb/issues/316)) ([19712ee](https://github.com/choiceoh/Deneb/commit/19712ee5a3e7cdb03feedc340735b36da48a3021))
* **gateway-go:** fix Telegram chat handler bugs — unique request IDs, reply timeouts, strict channel filter ([#311](https://github.com/choiceoh/Deneb/issues/311)) ([3a96b01](https://github.com/choiceoh/Deneb/commit/3a96b0123850a9311adf0010cba80acf6f8c868f))
* harden Go/Rust FFI build — buffer growth, handle safety, error codes ([#298](https://github.com/choiceoh/Deneb/issues/298)) ([93c68a6](https://github.com/choiceoh/Deneb/commit/93c68a68281eb8a2f26151ac266f56c248c91bbb))

## [3.10.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.9.0...deneb-v3.10.0) (2026-03-25)

### Features

- port 11 OpenClaw 3.22/3.23 features (security, performance, Telegram) ([#294](https://github.com/choiceoh/Deneb/issues/294)) ([9ab2fe6](https://github.com/choiceoh/Deneb/commit/9ab2fe6976238aab43bccd0035786a2de4fbfa15))

## [3.9.0](https://github.com/choiceoh/Deneb/compare/deneb-v3.8.0...deneb-v3.9.0) (2026-03-25)

### Features

- add Highway — Rust-native high-performance test orchestration engine ([#159](https://github.com/choiceoh/Deneb/issues/159)) ([8fc32a1](https://github.com/choiceoh/Deneb/commit/8fc32a14e99415baa4e1ef6898420c3b5771fd67))
- add Rust core library (core-rs) with napi-rs bindings ([#168](https://github.com/choiceoh/Deneb/issues/168)) ([49056eb](https://github.com/choiceoh/Deneb/commit/49056ebcc139c53b7e5b241d2fe65742267cfca2))
- centralize version management and release notes ([#54](https://github.com/choiceoh/Deneb/issues/54)) ([a1f94b3](https://github.com/choiceoh/Deneb/commit/a1f94b39911753bf0e0769214bf178db19f5e2ec))
- compaction notification via Telegram on compact ([6b19a08](https://github.com/choiceoh/Deneb/commit/6b19a084211680dd814747915b9aa3070b5e0776))
- dynamic channel registry (PoC) ([50b8a84](https://github.com/choiceoh/Deneb/commit/50b8a8485a724e5c6a78ad88e43488f3a292d4c0))
- replace lobster 🦞 with blue star ⭐ branding ([5c55012](https://github.com/choiceoh/Deneb/commit/5c550129001fd13e463d2e8cf727f66c0221a593))
- **vega:** implement command registry, search router, and SQLite FTS engine ([#272](https://github.com/choiceoh/Deneb/issues/272)) ([a85d431](https://github.com/choiceoh/Deneb/commit/a85d431beca53c003817b731751d583f166a134a))

### Bug Fixes

- add backward compatibility for OPENCLAW\_\* env vars and ~/.openclaw/ path ([d21d216](https://github.com/choiceoh/Deneb/commit/d21d2168941ab5d58084de5b8f2f83ed90cd0016))
- add clean:true to tsdown config to prevent stale dist artifacts breaking rebuilds ([849e8ee](https://github.com/choiceoh/Deneb/commit/849e8ee05b7d86c41c528fc8f4d93d0b4b97c5b0))
- add missing shouldSpawnWithShell export and fix process tool supervisor test mocking ([#146](https://github.com/choiceoh/Deneb/issues/146)) ([c54e081](https://github.com/choiceoh/Deneb/commit/c54e0818452e07c70b0857d669fd1aa247d35476))
- add rolldown plugin to prefer .ts over stale .js in extensions/ ([1f28a29](https://github.com/choiceoh/Deneb/commit/1f28a2915bc43ae789826adeb59d26e7f5954cf1))
- address critical bugs in 4 core modules ([#156](https://github.com/choiceoh/Deneb/issues/156)) ([d3620a7](https://github.com/choiceoh/Deneb/commit/d3620a7b16c3b90c9b8bd52fe63cb67574d0a01f))
- address potential runtime bugs in LCM engine and VegaMemoryManager ([eeb9f5f](https://github.com/choiceoh/Deneb/commit/eeb9f5f6b4f1aa51a6aaaeb9aee29898389c9636))
- auto-clean stale .js files in extensions/ before build ([1f192fa](https://github.com/choiceoh/Deneb/commit/1f192faf4cf4ba0fdb34c4658b6d82b71f51f626))
- autonomous tool schema duplicate enum + goal seeding when no active goals ([#71](https://github.com/choiceoh/Deneb/issues/71)) ([a6bf967](https://github.com/choiceoh/Deneb/commit/a6bf967776f79234e2bdf40235bafdfbf8c41590))
- await floating promises, log silent catch errors in gateway and memory ([8bae89a](https://github.com/choiceoh/Deneb/commit/8bae89a7e2a149eb0c847daea9d8e146978f31b0))
- channel plugin bugs — uninitialized sendPayload result, stuck status reactions, registry cache miss ([#64](https://github.com/choiceoh/Deneb/issues/64)) ([6599981](https://github.com/choiceoh/Deneb/commit/65999819738c58546070239394f4074dd7d83cf9))
- clean up stale references and build artifacts from CLI removal ([#242](https://github.com/choiceoh/Deneb/issues/242)) ([e342d3c](https://github.com/choiceoh/Deneb/commit/e342d3cabddafe87e6de3ad8730e9435a32a87b7))
- clear stale active-run state on SIGUSR1 restart to prevent queued utterances ([69d3bb7](https://github.com/choiceoh/Deneb/commit/69d3bb7eec3a2aa72b457bbc62dad29581b8d46e))
- code review — improve casts, validate role, fix dead code and stub types ([30bfacb](https://github.com/choiceoh/Deneb/commit/30bfacb272e00138d325adfcfbfe02a7ef9570f8))
- complete openclaw→deneb rebrand in wizard, scripts, and test fixtures ([45ece83](https://github.com/choiceoh/Deneb/commit/45ece839855bee656d4132820da3261030fb46cd))
- convert runtime module facades to proper lazy-loading boundaries ([#128](https://github.com/choiceoh/Deneb/issues/128)) ([53faa6d](https://github.com/choiceoh/Deneb/commit/53faa6de11dc42eca2bc20109399100db4ad1077))
- correct payload filter logic and cap debounce retries (PR[#108](https://github.com/choiceoh/Deneb/issues/108) regression) ([#126](https://github.com/choiceoh/Deneb/issues/126)) ([f5475a4](https://github.com/choiceoh/Deneb/commit/f5475a4027af45bd2343ad43557ef215f69b0270))
- ensure typing indicator cleanup on pre-runner errors ([#188](https://github.com/choiceoh/Deneb/issues/188)) ([b01321d](https://github.com/choiceoh/Deneb/commit/b01321d82282e4b32cea1c68345c45be47269e1c))
- explicitly rm -rf dist/ before tsdown build to prevent stale chunk resolution ([02c0cec](https://github.com/choiceoh/Deneb/commit/02c0cec314e522dffb4946e49992f23b0614df1b))
- fail-fast abort in parallel-check, reduce false-positive dependents in dev-affected ([#152](https://github.com/choiceoh/Deneb/issues/152)) ([98fd166](https://github.com/choiceoh/Deneb/commit/98fd1666750fc51ca0bc33224b20ebd612529e13))
- fix release-please config and sync all versions to 3.8.0 ([#254](https://github.com/choiceoh/Deneb/issues/254)) ([55f4696](https://github.com/choiceoh/Deneb/commit/55f4696b3e486f4b3c012aff650fd2f79f808e3f))
- harden path traversal checks and make JSON writes atomic ([#86](https://github.com/choiceoh/Deneb/issues/86)) ([e877594](https://github.com/choiceoh/Deneb/commit/e87759486273aeb627870580c1887490e8e84693))
- improve maintainability with type splits, error handling, and type safety ([#61](https://github.com/choiceoh/Deneb/issues/61)) ([1aa9465](https://github.com/choiceoh/Deneb/commit/1aa9465da92446a1ff5cd192714cc4b96a0ef9e1))
- link @deneb/native as file dependency and remove orphaned native/ ([#271](https://github.com/choiceoh/Deneb/issues/271)) ([95b2d09](https://github.com/choiceoh/Deneb/commit/95b2d0947f0f5aa6c889e3d80fb7d0cf126c4036))
- migrate lossless-claw plugin entry keys into config sub-object ([ee45235](https://github.com/choiceoh/Deneb/commit/ee452352ba4bacb0c785953ca81c598c17ddc413))
- normalize legacy qmd-wrapper.sh command to bare qmd ([bd5b15e](https://github.com/choiceoh/Deneb/commit/bd5b15e33716cee544282b13c7ea57d6958b8c57))
- observer compression ratio hitting leaf-level caps (~3%) instead of targetRatio (10-20%) ([#72](https://github.com/choiceoh/Deneb/issues/72)) ([00d79eb](https://github.com/choiceoh/Deneb/commit/00d79eb88510bc52dfdbf6e4dd31cbf2ecf23845))
- PR[#26](https://github.com/choiceoh/Deneb/issues/26)/[#27](https://github.com/choiceoh/Deneb/issues/27) runtime fixes + v3.5 ([32003a8](https://github.com/choiceoh/Deneb/commit/32003a814c065048a907b65cea076a99996c7bdf))
- preserve allowlist prefix stripping for removed channels + safe contract registry ([c0916e4](https://github.com/choiceoh/Deneb/commit/c0916e40056899f02711e382d93045ffc95e5964))
- preserve Anthropic thinking block order during empty-text filtering, decouple memory-core tool registration ([#127](https://github.com/choiceoh/Deneb/issues/127)) ([04d3a89](https://github.com/choiceoh/Deneb/commit/04d3a899ebc96b179200ee1daec90792219aaa71))
- preserve orphaned user messages for re-queue instead of silently dropping them ([#232](https://github.com/choiceoh/Deneb/issues/232)) ([acd506c](https://github.com/choiceoh/Deneb/commit/acd506cabc7f5ebda367eb3f6e86769c23fcf283))
- prevent agent response cutoff by adding error handling and safety-net to chat finalization ([#131](https://github.com/choiceoh/Deneb/issues/131)) ([91af832](https://github.com/choiceoh/Deneb/commit/91af832e2ff00f2ca4890ea875503d6456b8f583))
- prevent input swallowing and output loss during compaction and active runs ([#108](https://github.com/choiceoh/Deneb/issues/108)) ([ddbb68f](https://github.com/choiceoh/Deneb/commit/ddbb68fe4ccb15ad073a761e300b26dcc5bf5ed5))
- prevent session JSONL bloat degrading response speed and quality ([#66](https://github.com/choiceoh/Deneb/issues/66)) ([d64e6fa](https://github.com/choiceoh/Deneb/commit/d64e6fa030e3270b3438b0559239f95309494f87))
- prevent silent message drops in chat-to-agent delivery pipeline ([#218](https://github.com/choiceoh/Deneb/issues/218)) ([d83245a](https://github.com/choiceoh/Deneb/commit/d83245a6ce3bc621748ca3b57cb2b982e691392b))
- prevent test suite hang from sync jiti compilation of channel-runtime barrel ([#68](https://github.com/choiceoh/Deneb/issues/68)) ([3c5c4ac](https://github.com/choiceoh/Deneb/commit/3c5c4acabb2adb5f12e4a81351c8c7f1d5ab979d))
- remove as-any casts and extract per-channel audit functions for maintainability ([66a0f6f](https://github.com/choiceoh/Deneb/commit/66a0f6f26e63b7d17f1f8279ea99bb016bf6fdfc))
- remove broken jiti integration tests and fix context engine reserved id in loader tests ([#79](https://github.com/choiceoh/Deneb/issues/79)) ([55ecd6e](https://github.com/choiceoh/Deneb/commit/55ecd6ea9c3ffbcd0637a8abb9b147f6c49e2ee6))
- remove orphaned speech plugin-sdk entrypoints breaking build ([#265](https://github.com/choiceoh/Deneb/issues/265)) ([fa6f507](https://github.com/choiceoh/Deneb/commit/fa6f507d99f1b63f5845be8c7c1a35ac0e1ea91d))
- remove stale daemon-cli build entries left after CLI removal ([#234](https://github.com/choiceoh/Deneb/issues/234)) ([#240](https://github.com/choiceoh/Deneb/issues/240)) ([7ac7b87](https://github.com/choiceoh/Deneb/commit/7ac7b879286fdad1d3a024d190afadf59a4bb98b))
- remove stale extension references from CI lint scripts and guard tests ([681123e](https://github.com/choiceoh/Deneb/commit/681123e8550aa165a83343dd50951e47f4506c9f))
- replace as-any casts with proper types, remove dead Discord runtime, guard plugin.config access ([72c6f03](https://github.com/choiceoh/Deneb/commit/72c6f03af589fb52e1d0310c0b59789ebfcedf2c))
- replace stale eslint-disable comments with oxlint-disable and remove unnecessary suppressions ([#138](https://github.com/choiceoh/Deneb/issues/138)) ([8e3ab12](https://github.com/choiceoh/Deneb/commit/8e3ab126bac8c0f2f7d9fb8a0a0c2dc9f7f799c4))
- reset followup queue drain state on SIGUSR1 restart ([fc070c9](https://github.com/choiceoh/Deneb/commit/fc070c926abe49e0b0dc7cf1efa356d30bd55467))
- resolve 4 [@ts-expect-error](https://github.com/ts-expect-error) suppressions for strict mode ([#174](https://github.com/choiceoh/Deneb/issues/174)) ([701d73c](https://github.com/choiceoh/Deneb/commit/701d73c9081ccb0684d1e59687f917c6d5e4438f))
- resolve agent module bugs — security hardening, stale mock paths, missing tool ([#149](https://github.com/choiceoh/Deneb/issues/149)) ([d33a4c8](https://github.com/choiceoh/Deneb/commit/d33a4c80fa6ff95c283dd07d14be9081d200f79a))
- resolve build and test bugs across native addon, markdown, and config ([#264](https://github.com/choiceoh/Deneb/issues/264)) ([a7a824c](https://github.com/choiceoh/Deneb/commit/a7a824c1caa141cb4e2b6cde0f1a5b5e2d916dfc))
- resolve infra module test failures across 9 test files ([#161](https://github.com/choiceoh/Deneb/issues/161)) ([2d2a977](https://github.com/choiceoh/Deneb/commit/2d2a9774b2fbc903ef84ca3b58170f6a760353fe))
- resolve master check errors (missing swift policy script, format, stale plugin-sdk exports) ([#39](https://github.com/choiceoh/Deneb/issues/39)) ([eac92ae](https://github.com/choiceoh/Deneb/commit/eac92aea8163787750e4563bbe65e06266cbba47))
- resolve PR [#184](https://github.com/choiceoh/Deneb/issues/184) merge bugs — missing dep, workspace profiles, CI gaps ([#200](https://github.com/choiceoh/Deneb/issues/200)) ([d0f889e](https://github.com/choiceoh/Deneb/commit/d0f889e1851018cbe28ca730e33d50745223925c))
- resolve PR[#195](https://github.com/choiceoh/Deneb/issues/195) merge bugs ([#197](https://github.com/choiceoh/Deneb/issues/197)) ([9208ead](https://github.com/choiceoh/Deneb/commit/9208ead95c6b9519c98e2dc82ebea733432e4ad1))
- resolve recent merge bugs — duplicate Go map keys, stale provider field, wrong bridge call, highway binary path ([#209](https://github.com/choiceoh/Deneb/issues/209)) ([083b698](https://github.com/choiceoh/Deneb/commit/083b698246a357ff291dd663369dd132024b9bc2))
- resolve remaining stub root causes — inline dead stubs, remove dead telegram pairing migration ([#90](https://github.com/choiceoh/Deneb/issues/90)) ([b168e36](https://github.com/choiceoh/Deneb/commit/b168e361c97658a5a7c74b7edb6b76e06618ed02))
- resolve Rust/Go build errors — missing napi imports, FFI field mismatch, Go test params ([#217](https://github.com/choiceoh/Deneb/issues/217)) ([df5f454](https://github.com/choiceoh/Deneb/commit/df5f454e586989a5d2da3b5a1a581b063e8b92ff))
- resolve stub root causes — delete 18 pure-stub files, inline no-op behavior, fix secret-equal ([#57](https://github.com/choiceoh/Deneb/issues/57)) ([377a95f](https://github.com/choiceoh/Deneb/commit/377a95f81fd2e7aa319a4407c56cf2203e705476))
- resolve technical debt — add error logging to empty catch blocks, eliminate double casts, fix qmd-manager test mock mismatches ([#103](https://github.com/choiceoh/Deneb/issues/103)) ([781c40e](https://github.com/choiceoh/Deneb/commit/781c40e91eaa4df058ddcf1c5eeff69b9d5a1497))
- resolve three bugs in util modules ([#142](https://github.com/choiceoh/Deneb/issues/142)) ([0cec3b3](https://github.com/choiceoh/Deneb/commit/0cec3b3c04af1659083f7efc55c0e9fb1ed6ae58))
- restore device identity crypto and fix gateway test infrastructure ([#150](https://github.com/choiceoh/Deneb/issues/150)) ([a60dc8a](https://github.com/choiceoh/Deneb/commit/a60dc8afb9ff2a576d9da6af12170585d2fdd3a2))
- restore missing pw-session extracted modules and fix env-dependent test ([#58](https://github.com/choiceoh/Deneb/issues/58)) ([91b2b3f](https://github.com/choiceoh/Deneb/commit/91b2b3fc94952395b4359191db718e788996b6cc))
- restore WhatsApp normalize stub (required by outbound-session import) ([b70ee80](https://github.com/choiceoh/Deneb/commit/b70ee80e42fa5ec53ad1b9851dcce3b1fbbe1edb))
- simplify release-please to single package (fix linked-versions crash) ([#261](https://github.com/choiceoh/Deneb/issues/261)) ([11fa93c](https://github.com/choiceoh/Deneb/commit/11fa93cf92e5139e8f38b03c9dbb26761019e37c))
- sync version badges, fix Go bridge race, fix channel-starter types ([#277](https://github.com/choiceoh/Deneb/issues/277)) ([f3d32d9](https://github.com/choiceoh/Deneb/commit/f3d32d9a7022f3c88164088b5f8c075e9dd665cb))
- telegram network test — stub proxy env vars and correct timeout expectation ([#148](https://github.com/choiceoh/Deneb/issues/148)) ([73275dc](https://github.com/choiceoh/Deneb/commit/73275dceae92aebb18f66b0d4d2bdd373ce91e18))
- update package-lock.json with deneb package name and bin entry ([af6037a](https://github.com/choiceoh/Deneb/commit/af6037a2b29111e4699f523989942d0da3119fe5))
- update package.json bin/files/exports to reference deneb.mjs instead of missing openclaw.mjs ([87796e3](https://github.com/choiceoh/Deneb/commit/87796e3de12aaed5f0094103ecad4a8ac37dd780))
- update plugin test fixture archives to use deneb branding ([309840d](https://github.com/choiceoh/Deneb/commit/309840da99bbdfe12a73b0908bd9a694a91a35a6))
- use ctx.hookRunner instead of broken vi.mock for after_tool_call e2e test, add missing successfulCronAdds to test helper ([#145](https://github.com/choiceoh/Deneb/issues/145)) ([fcb7d13](https://github.com/choiceoh/Deneb/commit/fcb7d131711653a38fa6d33d7834a1b3acb5aa60))
- use git add -f in pre-commit hook to handle tracked files in gitignored dirs ([#70](https://github.com/choiceoh/Deneb/issues/70)) ([cc09cb7](https://github.com/choiceoh/Deneb/commit/cc09cb72a9a87fb0fe7919968af6ce3616cbd913))
- use import.meta.dirname instead of process.cwd() for reliable file path resolution ([34a9b44](https://github.com/choiceoh/Deneb/commit/34a9b44c2e461fea511c253c1d3f3ccb237db65f))
- use relative symlinks for dist-runtime extension node_modules ([#144](https://github.com/choiceoh/Deneb/issues/144)) ([9064844](https://github.com/choiceoh/Deneb/commit/90648449c254e08e972dfbe1d6f06a49396334ee))
- v3.150 bugfixes — resolve TS errors, lint, and broken extension references ([3af9c07](https://github.com/choiceoh/Deneb/commit/3af9c07f4427835c62f24769c70b0fd4e01339ec))

## [3.5.7](https://github.com/choiceoh/Deneb/compare/v3.2.0...v3.5.7) (2026-03-23)

프로젝트 문서 전면 최신화.

### Features

- **프로젝트 문서 최신화** — README, VISION, CONTRIBUTING, SECURITY, CHANGELOG를 현재 v3.5.7 기준으로 전면 갱신
- **Node.js 요구사항 통일** — 최소 22.16.0, 권장 Node 24로 전 문서 일치화
- **코드베이스 규모 갱신** — 실측 기준 ~440K LOC 반영
- **개발 커맨드 갱신** — `pnpm check` (oxlint + oxfmt) 기준으로 통일
- **아키텍처 다이어그램 갱신** — 현재 `src/` 디렉토리 구조 반영 (plugin-sdk, routing, tts, web-search, vega)

## [3.2.0](https://github.com/choiceoh/Deneb/compare/v3.0.0...v3.2.0) (2026-03-21)

ACP (Claude Code) 연동 활성화, 코드 구조 리팩토링.

### Features

- **ACP/Claude Code 연동** — acpx 플러그인 활성화, `acp.allowedAgents` 설정
- **Refactor** — 대형 파일에서 9개 전용 모듈 추출 (PR #22)

## [3.0.0](https://github.com/choiceoh/Deneb/releases/tag/v3.0.0) (2026-03-21)

Deneb 최초 릴리스. 독립 프로젝트로 시작.

### Features

- **Aurora Memory Module** — AI-agent-first 메모리 파일 관리 (memory-md-manager)
- **Vega 메모리 백엔드 통합** — VegaMemoryManager, Aurora 네이티브화
- **Vega CLI 래퍼** — bin/vega wrapper + install.sh
- **Aurora Context Engine** — DAG-based compaction, background observer, multi-layer recall
- **컨텍스트 엔진** — transcript maintenance 기능
- **Telegram custom apiRoot** 지원
- Telegram 전용 빌드 (미사용 채널 어댑터 제거)
- 에러 리질리언스 계층 추가
- Rolldown 빌드 안정화 (stale .js 정리, clean:true)
- Subagent 타임아웃 시 부분 진행 결과 포함
- JSONL 세션 로그 트렁케이션 (디스크 과다 사용 방지)
- Compaction 후 세션 JSONL 자동 트렁케이션

### Bug Fixes

- Telegram 스트리밍 미리보기 종료 시 message:sent hook 발행
- Telegram dmPolicy pairing 경고
- 시퀀스 갭 브로드캐스트 스킵
- 잘못된 형식의 replay tool call sanitize
- Thread binding 직렬화
- Telegram accountId 누락 시 잘못된 봇 라우팅 방지
