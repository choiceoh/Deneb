# Deneb Changelog

Deneb is a specialized fork of [OpenClaw](https://github.com/openclaw/openclaw), optimized for a **memory-first AI assistant** running on NVIDIA DGX Spark (ARM64) with Telegram and local QMD/Vega search.

For upstream OpenClaw changelogs, see [openclaw/openclaw](https://github.com/openclaw/openclaw).

---

## v3.151 (2026-03-21)

### Breaking

- **macOS, iOS, Android platform support removed** — All native app code, CI workflows, Swabble, and platform documentation deleted (-140K lines, 966 files). Deneb is now server-only (Linux ARM64).

### Changes

- **Vega search engine bundled** (`ext/vega/`) — Local hybrid search engine with SQLite + vector (cosine similarity) routing, Qwen3.5-9B query expansion, Qwen3-Embedding-8B, and Qwen3-Reranker-4B.
- **Vega memory backend** — `VegaMemoryManager` in `src/memory/vega-manager.ts` as alternative to QMD for `memorySearch.backend`.
- **Refactored sync message hooks** — Extracted shared `runSyncMessageHook` from duplicated `tool_result_persist` and `before_message_write` hooks (+75 -87 lines).
- **A2UI stub bundle** — Minimal stub for Telegram-only build (canvas UI removed in v3.150).
- **`.gitignore` updated** — Added `dist-runtime/`, model files, environment symlinks.

### Fixes

- Resolved 130+ TypeScript build errors from v3.150 channel/extension removal.
- Improved type safety: replaced `as-any` casts with proper types, guarded `plugin.config` access.
- Removed dead Discord runtime code from `session-reset-service.ts`.
- Fixed broken extension references in CI lint scripts.
- Cleaned up legacy allowlists and error handling.
- Added `isThenable` utility for hook runner reliability.

---

## v3.150 (2026-03-20)

### Breaking

- **LCM promoted to native core** — Lossless Context Management moved from plugin to `src/context-engine/lcm/`. Set `plugins.slots.contextEngine` to `"lcm"`.
- **Discord, Slack, WhatsApp, Signal, Matrix adapters removed** — Only Telegram and ACP remain.
- **36 unused built-in skills removed** — Including 1password, slack, discord, and others.

### Changes

- **Vega QMD backend integration** — `VegaMemoryManager` with `search()`, `search_fast()`, `search_semantic()` methods.
- **FallbackMemoryManager** — Auto-fallback from QMD to builtin `MemoryIndexManager` on primary failure.
- **Vega status caching** — 30-second TTL for `vega memory-status` calls.

---

## v3.143 (2026-03-19)

- `.gitignore` created and applied.
- 36 unused built-in skills removed from `src/skills/`.

---

## v3.142 (2026-03-18)

- Upstream sync from OpenClaw 2026.3.13 release.

---

## v3.141 (2026-03-17)

- `server.impl.ts` split into smaller modules for maintainability.
- Initial fork divergence from upstream OpenClaw.
