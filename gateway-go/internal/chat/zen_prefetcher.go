// zen_prefetcher.go — Zen arch: Hardware Prefetcher for predictive data loading.
//
// CPU analogy: Hardware prefetchers monitor memory access patterns and
// speculatively load cache lines before the CPU requests them. This hides
// memory latency by overlapping data fetch with computation.
//
// Application: After each agent run completes, the prefetcher predicts what
// data the NEXT run in this session will need and pre-loads it into caches:
//
//   1. Sglang health — pre-check local LLM availability (avoids 50-200ms probe at run start)
//   2. Context files — pre-load workspace files into the mtime cache
//   3. Transcript warmup — touch the transcript cache to keep it hot
//
// The prefetcher runs asynchronously after handleRunSuccess, overlapping with
// the user's think time before their next message.
package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
)

// prefetchTimeout caps total prefetch work to avoid wasting resources
// if the user doesn't send another message soon.
const prefetchTimeout = 10 * time.Second

// PrefetchForNextRun speculatively pre-loads data likely needed by the next
// agent run in this session. Safe to call as a goroutine — all operations
// are best-effort and timeout-bounded.
//
// This is the software equivalent of a hardware prefetcher: it observes the
// "access pattern" (a completed run) and predicts the next access (another
// run in the same session with similar workspace/context).
func PrefetchForNextRun(
	ctx context.Context,
	sessionKey string,
	workspaceDir string,
	deps runDeps,
	logger *slog.Logger,
) {
	ctx, cancel := context.WithTimeout(ctx, prefetchTimeout)
	defer cancel()

	// Prefetch 1: Sglang health probe.
	// After a successful run, the next run is likely to need proactive context
	// (sglang local LLM). Pre-check health so the run doesn't waste time probing.
	go prefetchSglangHealth()

	// Prefetch 2: Context files.
	// Workspace context files (CLAUDE.md, SOUL.md, etc.) are cached with mtime
	// validation. Touch the cache now so the next prompt build gets a cache hit
	// instead of re-reading from disk.
	if workspaceDir != "" {
		go func() {
			_ = prompt.LoadContextFiles(workspaceDir)
		}()
	}

	// Prefetch 3: Transcript warmup.
	// The transcript cache has a 10s TTL. Loading now extends the window so
	// the next context assembly likely gets a cache hit.
	if deps.transcript != nil {
		go func() {
			_, _, _ = deps.transcript.Load(sessionKey, 0)
		}()
	}

	// Prefetch 4: Knowledge embedding warmup.
	// If the memory embedder is available, pre-load the embedding model state
	// by running a tiny embedding request. This warms up the GGUF model on
	// DGX Spark so the next semantic search is faster.
	if deps.memoryEmbedder != nil {
		go func() {
			embedCtx, embedCancel := context.WithTimeout(ctx, 3*time.Second)
			defer embedCancel()
			// Minimal embedding to warm the model — result is discarded.
			_, _ = deps.memoryEmbedder.EmbedQuery(embedCtx, "warmup")
		}()
	}

	logger.Debug("prefetcher: scheduled next-run prefetch", "session", sessionKey)
}

// prefetchSglangHealth updates the cached sglang health status.
// Uses checkSglangHealth from toolreg_pilot_sglang.go which updates
// the package-level sglangHealthy and sglangLastCheck atomics.
func prefetchSglangHealth() {
	checkSglangHealth()
}
