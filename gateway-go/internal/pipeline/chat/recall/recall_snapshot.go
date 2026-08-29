package recall

import (
	"context"
	"log/slog"
	"time"
)

// SnapshotOptions carries parent-owned side effects around recall preflight.
type SnapshotOptions struct {
	// OnExplicitRecall is called for a cue turn whose snapshot was not served
	// from cache. The parent chat runner owns user-visible phase emission; recall
	// owns deciding when the expensive preflight is really going to run.
	OnExplicitRecall func(at time.Time)
}

// BuildSnapshot returns the wire-ready recall block for a turn, including
// preflight suppression, per-cue cache lookup, cache-safe citation re-arming,
// and first-write-wins snapshot freezing. Parent chat orchestration should call
// this high-level entry point rather than coordinating cache internals itself.
func BuildSnapshot(ctx context.Context, params Params, deps Deps, options SnapshotOptions, logger *slog.Logger) string {
	if params.recallSuppressed() {
		return ""
	}

	fingerprint := cueFingerprint(params.Message)
	hasCue := fingerprint != ""
	cacheableCue := hasCue && !needsContextRewrite(params.Message)
	var recallCacheGeneration uint64

	if hasCue && !deps.Briefcase {
		if cacheableCue {
			cached, ok, generation := cachedSnapshotWithGeneration(params.SessionKey, fingerprint)
			recallCacheGeneration = generation
			if ok {
				// A snapshot-served turn still shows the model the pinned wiki
				// evidence: re-arm the citation pass so later turns of a frozen
				// conversation can earn cite events.
				armSnapshotCitations(params.SessionKey, cached)
				return cached
			}
		}
		if options.OnExplicitRecall != nil {
			options.OnExplicitRecall(time.Now())
		}
	}

	recallMemory, recallTruncated := Build(ctx, params, deps, logger)
	if !deps.Briefcase && cacheableCue && shouldFreeze(hasCue, recallTruncated, recallMemory) {
		storeSnapshotIfGeneration(params.SessionKey, fingerprint, recallMemory, recallCacheGeneration)
	}
	return recallMemory
}
