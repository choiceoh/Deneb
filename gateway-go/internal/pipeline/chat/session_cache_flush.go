package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// flushSessionPromptCaches drops the per-session prompt snapshots that may hold
// a wiki page's content: the frozen context-file snapshot, the recall snapshot,
// the tier-1 wiki snapshot, and their restart-surviving persisted copy. Shared
// by /reset (handleResetCommand) and wiki_forget so a hard-deleted page cannot
// re-surface in the same session's prompt from a frozen snapshot after it is
// gone from the live wiki. No-op for an empty session key.
func flushSessionPromptCaches(sessionKey string) {
	if sessionKey == "" {
		return
	}
	prompt.ClearSessionSnapshot(sessionKey) // context-file session snapshot
	leafbind.RecallClearSession(sessionKey) // recall snapshot cache
	clearTier1Wiki(sessionKey)              // tier-1 wiki snapshot
	forgetPromptSnapshot(sessionKey)        // persisted (restart-surviving) copy
}
