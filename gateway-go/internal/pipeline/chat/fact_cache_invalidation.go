package chat

import (
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
)

// factDerivedCacheMu serializes startup snapshot restoration with a fact
// invalidation. Server readiness precedes asynchronous snapshot restore, so a
// mutation can otherwise clear the live stores while restore puts stale
// persisted bytes back immediately afterward.
var factDerivedCacheMu sync.Mutex

// ClearFactDerivedCachesAtRevision invalidates every session-frozen projection
// of shared fact state. Fact writes are rare, so the one-turn cache rebuild cost
// is worth making a correction visible consistently across main, sub, cron, and
// system sessions instead of only in the writer's session.
func ClearFactDerivedCachesAtRevision(revision uint64, projectionError string) {
	factDerivedCacheMu.Lock()
	defer factDerivedCacheMu.Unlock()

	clearLiveFactDerivedCaches()
	// A canonical mutation may commit even when its MEMORY.md/USER.md projection
	// fails. Clear live caches either way, but keep persisted fact-derived fields
	// disabled until a healthy mutation or successful startup explicitly approves
	// a revision. Topic knowledge remains independently persistable.
	clearFactPromptSnapshots(revision, projectionError == "")
}

// DisableFactDerivedCaches invalidates fact-derived prompt state without
// approving any journal revision. Fatal/ambiguous fact-journal failures use it
// before shutdown because there is no revision safe to stamp onto new persisted
// Tier1 or context bytes. Topic knowledge is independent and remains cached.
func DisableFactDerivedCaches() {
	factDerivedCacheMu.Lock()
	defer factDerivedCacheMu.Unlock()

	clearLiveFactDerivedCaches()
	disableFactPromptSnapshots()
}

func clearLiveFactDerivedCaches() {
	// The persisted mirror advances the shared generation after these live stores
	// are cleared. A racing persistence attempt can only lose one harmless write;
	// any turn holding the old generation is then rejected.
	chatrecall.ClearAll()
	clearAllTier1Wiki()
	prompt.ClearAllContextSnapshots()
}
