// steer.go — Per-session /steer queue for mid-run nudges on the main agent.
//
// Ported from Hermes Agent's _pending_steer mechanism (run_agent.py). A steer
// is a short user note injected into the running agent's NEXT tool-result
// message without breaking the turn or the prompt cache.
//
// Why this exists: the user sometimes wants to nudge the agent mid-tool-call
// ("actually, skip the tests" / "also update the changelog") without aborting
// and restarting the run. Interrupting kills in-progress tool state; queuing
// for after-run feels too late when the agent is about to go off-course.
//
// Design (mirrors Hermes lines 963-964, 3708-3758, 9060-9108):
//
//  1. Producer (RPC / slash command) calls SteerQueue.Enqueue(sessionKey, note).
//     Multiple concurrent steers are concatenated with newlines.
//  2. Consumer (chat pipeline) calls Drain(sessionKey) right before each LLM
//     API call. If text is returned, the caller appends it to the LAST
//     tool-result-bearing message in the outgoing message list.
//  3. If no tool-result message exists yet (turn 0, no tools run yet), the
//     caller returns the drained notes via Restore(sessionKey, notes) so the
//     next turn (after the first tool batch) picks them up. This preserves
//     role alternation — we never insert a fresh user turn.
//
// Concurrency model:
//   - One sync.Mutex for the entire queue map. The critical section is tiny
//     (slice append / copy-and-clear), so a single lock is simpler and faster
//     than a sync.Map of per-session locks at this call frequency (a few per
//     session per minute at most).
//   - Never holds the lock while calling external code.
//   - Producers and consumers are different goroutines (RPC handler vs. agent
//     run goroutine), so the lock keeps reads and writes consistent.
//
// Prompt-cache safety: steer notes attach to an EXISTING tool-result block's
// content, never create a new system-prompt block. This keeps the cache
// prefix stable across turns (Anthropic and OpenAI prompt caching both key
// on the system prompt + leading user messages).
package chat

import (
	"strings"
	"sync"
)

// SteerQueue is a thread-safe per-session queue of pending /steer notes.
// One queue instance is shared by the chat Handler across all sessions.
type SteerQueue struct {
	mu    sync.Mutex
	items map[string][]string // sessionKey -> ordered notes
}

// NewSteerQueue returns a ready-to-use SteerQueue.
func NewSteerQueue() *SteerQueue {
	return &SteerQueue{items: make(map[string][]string)}
}

// Enqueue appends a note to the pending-steer buffer for sessionKey.
// Empty / whitespace-only notes are ignored and the method returns false.
// Returns true if the note was accepted.
//
// Safe to call from any goroutine.
func (q *SteerQueue) Enqueue(sessionKey, note string) bool {
	cleaned := strings.TrimSpace(note)
	if cleaned == "" || sessionKey == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items[sessionKey] = append(q.items[sessionKey], cleaned)
	return true
}

// Drain removes and returns every pending note for sessionKey, in enqueue
// order. Returns nil when nothing is queued. The caller is expected to
// actually consume them (inject into a tool-result message); when that
// injection is not possible (no tool-result yet), call Restore with the same
// slice so the notes are not lost.
//
// Safe to call from any goroutine.
func (q *SteerQueue) Drain(sessionKey string) []string {
	if sessionKey == "" {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	notes := q.items[sessionKey]
	if len(notes) == 0 {
		return nil
	}
	delete(q.items, sessionKey)
	return notes
}

// Restore puts notes back onto the front of the queue. Used when Drain was
// called but the caller could not find a tool-result message to inject
// into — we want the notes to survive until the next tool batch produces a
// valid injection target (mirrors Hermes' post-tool-result re-enqueue path).
//
// Restored notes preserve their original order and sit ahead of any notes
// that arrived after the drain call.
//
// Safe to call from any goroutine.
func (q *SteerQueue) Restore(sessionKey string, notes []string) {
	if sessionKey == "" || len(notes) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	existing := q.items[sessionKey]
	// Prepend: restored notes must land before newcomers so steer order matches
	// user intent (first note sent is first note seen by the model).
	combined := make([]string, 0, len(notes)+len(existing))
	combined = append(combined, notes...)
	combined = append(combined, existing...)
	q.items[sessionKey] = combined
}

// Clear removes all pending notes for sessionKey (used by /reset handling).
//
// Safe to call from any goroutine.
func (q *SteerQueue) Clear(sessionKey string) {
	if sessionKey == "" {
		return
	}
	q.mu.Lock()
	delete(q.items, sessionKey)
	q.mu.Unlock()
}

// Len returns the number of queued notes for sessionKey. Primarily useful
// for tests and diagnostics.
func (q *SteerQueue) Len(sessionKey string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items[sessionKey])
}

// Reset wipes the entire queue across all sessions.
func (q *SteerQueue) Reset() {
	q.mu.Lock()
	q.items = make(map[string][]string)
	q.mu.Unlock()
}

// steerMarkerPrefix is the Korean-first label prepended to steer text when
// it is attached to a tool_result. Marker is inside a tool_result content
// string (not a new message), so the model sees it as additional context for
// that tool's output, not as a new user turn. "사용자 조정" ("user steer")
// matches the codebase's Korean-first convention while still being clear
// enough that the model will not mistake it for a real tool result line.
const steerMarkerPrefix = "\n\n[사용자 조정: "

// steerMarkerSuffix closes the marker block.
const steerMarkerSuffix = "]"

// formatSteerMarker returns the injection text for a set of notes. Notes
// are joined with " / " so multi-steer bursts read as a single annotation.
func formatSteerMarker(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	// Filter out empty elements defensively (Enqueue already trims).
	clean := make([]string, 0, len(notes))
	for _, n := range notes {
		n = strings.TrimSpace(n)
		if n != "" {
			clean = append(clean, n)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return steerMarkerPrefix + strings.Join(clean, " / ") + steerMarkerSuffix
}
