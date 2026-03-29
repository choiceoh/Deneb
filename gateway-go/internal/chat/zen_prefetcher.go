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

	// All prefetch work runs inline (not as child goroutines) because this
	// function is already called as a goroutine. The timeout context bounds
	// total work; defer cancel() safely fires after all work completes.

	// Prefetch 1: Sglang health probe.
	prefetchSglangHealth()

	// Prefetch 2: Context files — touch the mtime cache.
	if workspaceDir != "" {
		_ = prompt.LoadContextFiles(workspaceDir)
	}

	// Prefetch 3: Transcript warmup — extend the 10s TTL window.
	if deps.transcript != nil {
		_, _, _ = deps.transcript.Load(sessionKey, 0)
	}

	// Prefetch 4: Knowledge embedding model warmup (DGX Spark GPU).
	if deps.memoryEmbedder != nil {
		embedCtx, embedCancel := context.WithTimeout(ctx, 3*time.Second)
		_, _ = deps.memoryEmbedder.EmbedQuery(embedCtx, "warmup")
		embedCancel()
	}

	logger.Debug("prefetcher: next-run prefetch done", "session", sessionKey)
}

// prefetchSglangHealth updates the cached sglang health status.
// Uses checkSglangHealth from toolreg_pilot_sglang.go which updates
// the package-level sglangHealthy and sglangLastCheck atomics.
func prefetchSglangHealth() {
	checkSglangHealth()
}
