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

// ClearFactDerivedCaches invalidates every session-frozen projection of shared
// fact state. Fact writes are rare, so the one-turn cache rebuild cost is worth
// making a correction visible consistently across main, sub, cron, and system
// sessions instead of only in the writer's session.
func ClearFactDerivedCaches() {
	factDerivedCacheMu.Lock()
	defer factDerivedCacheMu.Unlock()

	// Advance each live store's generation first. A turn that began before the
	// correction can finish using its local snapshot, but cannot refill a cleared
	// cache. Clear the persisted mirror last: turns that observed old live bytes
	// necessarily carry the old prompt generation and are rejected, while turns
	// that race between the live clear and persisted clear only lose one harmless
	// persistence attempt.
	chatrecall.ClearAll()
	clearAllTier1Wiki()
	prompt.ClearAllContextSnapshots()
	// This also removes Tier1/MEMORY bytes from prompt_snapshots.json so a
	// gateway restart cannot restore pre-correction context.
	clearFactPromptSnapshots()
}
