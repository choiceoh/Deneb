// subagent_notify.go — Proactive parent notification on child session completion.
//
// When a child session (spawned via sessions_spawn) reaches a terminal state
// (done/failed/killed/timeout), this module notifies the parent session:
//
//   - Parent is running: pushes to the parent's subagent notification channel,
//     which is consumed by DeferredSystemText on the next agent turn.
//   - Parent is idle: enqueues a system notification via pendingMsgs and triggers
//     a new agent run so the parent can react to the child's result.
//
// Debounced queue: concurrent child completions within 1s are batched into a
// single notification to avoid flooding the parent with per-child messages.
// Overflow beyond cap (20) is summarized to prevent runaway queue growth.
//
// This eliminates the need for parent agents to poll subagent status.
package chat

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// subagentNotifyChCap is the buffer size for per-parent notification channels.
const subagentNotifyChCap = 20

// Debounced queue constants.
const (
	notifyDebounceMs = 1000 // 1s debounce window — batches concurrent completions.
	notifyQueueCap   = 20   // max pending notifications before overflow summarize.
)

// notifyQueue collects child completion notifications for a parent session
// and flushes them as a batch after the debounce window expires.
type notifyQueue struct {
	mu       sync.Mutex
	items    []notifyItem
	timer    *time.Timer
	flushFn  func(items []notifyItem) // called with collected items after debounce
	capacity int
}

// notifyItem is a single child completion event pending in the queue.
type notifyItem struct {
	childKey      string
	label         string
	status        session.RunStatus
	runtimeMs     int64
	failureReason string
	lastOutput    string
}

// enqueue adds a notification and resets the debounce timer. If the queue
// exceeds capacity, older items are dropped and replaced with a summary count.
func (q *notifyQueue) enqueue(item notifyItem) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Append unconditionally — flush handles overflow summarization.
	q.items = append(q.items, item)

	// Reset debounce timer.
	if q.timer != nil {
		q.timer.Stop()
	}
	q.timer = time.AfterFunc(time.Duration(notifyDebounceMs)*time.Millisecond, q.flush)
}

// flush drains the queue and calls flushFn with the collected items.
func (q *notifyQueue) flush() {
	q.mu.Lock()
	if len(q.items) == 0 {
		q.mu.Unlock()
		return
	}
	items := q.items
	q.items = nil
	q.mu.Unlock()

	q.flushFn(items)
}

// startSubagentNotifier subscribes to session lifecycle events and routes
// completion notifications to parent sessions via debounced queues.
// Called once from NewHandler.
func (h *Handler) startSubagentNotifier() {
	bus := h.sessions.EventBusRef()
	bus.Subscribe(func(event session.Event) {
		// Only care about status transitions to terminal states.
		if event.Kind != session.EventStatusChanged {
			return
		}
		if !isTerminalStatus(event.NewStatus) {
			return
		}

		child := h.sessions.Get(event.Key)
		if child == nil || child.SpawnedBy == "" {
			return
		}

		parentKey := child.SpawnedBy
		item := buildNotifyItem(child)

		q := h.getOrCreateNotifyQueue(parentKey)
		q.enqueue(item)

		h.logger.Info("subagent completion queued for parent",
			"child", abbreviateSession(event.Key),
			"parent", abbreviateSession(parentKey),
			"status", string(event.NewStatus))
	})
}

// getOrCreateNotifyQueue returns the debounced notification queue for a parent
// session, creating it lazily with a flush function that routes to the
// appropriate delivery path (DeferredSystemText or pendingMsgs).
func (h *Handler) getOrCreateNotifyQueue(parentKey string) *notifyQueue {
	h.subagentNotifyMu.Lock()
	defer h.subagentNotifyMu.Unlock()

	if q, ok := h.subagentNotifyQueues[parentKey]; ok {
		return q
	}

	q := &notifyQueue{
		capacity: notifyQueueCap,
		flushFn: func(items []notifyItem) {
			notification := formatBatchNotification(items)

			if h.hasActiveRunForSession(parentKey) {
				// Parent is running: inject via DeferredSystemText.
				h.pushSubagentNotification(parentKey, notification)
			} else {
				// Parent is idle: trigger a new run.
				h.triggerSubagentNotificationRun(parentKey, notification)
			}

			h.logger.Info("subagent batch notification flushed",
				"parent", abbreviateSession(parentKey),
				"count", len(items),
				"parentRunning", h.hasActiveRunForSession(parentKey))
		},
	}
	h.subagentNotifyQueues[parentKey] = q
	return q
}

// getOrCreateSubagentNotifyCh returns the notification channel for a parent
// session, creating it lazily if needed.
func (h *Handler) getOrCreateSubagentNotifyCh(sessionKey string) chan<- string {
	h.subagentNotifyMu.Lock()
	defer h.subagentNotifyMu.Unlock()
	ch, ok := h.subagentNotifyChs[sessionKey]
	if !ok {
		ch = make(chan string, subagentNotifyChCap)
		h.subagentNotifyChs[sessionKey] = ch
	}
	return ch
}

// subagentNotifyCh returns a read-only view of the notification channel for a
// parent session, or nil if none exists. Used by DeferredSystemText composition.
func (h *Handler) subagentNotifyCh(sessionKey string) <-chan string {
	h.subagentNotifyMu.Lock()
	defer h.subagentNotifyMu.Unlock()
	ch := h.subagentNotifyChs[sessionKey]
	if ch == nil {
		return nil
	}
	return ch
}

// pushSubagentNotification sends a notification to the parent's channel.
// Non-blocking: if the channel is full, the notification is dropped with a
// warning log (parent will still see the child's status via subagents tool).
func (h *Handler) pushSubagentNotification(parentKey, notification string) {
	ch := h.getOrCreateSubagentNotifyCh(parentKey)
	select {
	case ch <- notification:
	default:
		h.logger.Warn("subagent notification channel full, dropping",
			"parent", abbreviateSession(parentKey))
	}
}

// triggerSubagentNotificationRun starts a run for an idle parent session.
// The agent receives the batch notification as a system-injected message.
func (h *Handler) triggerSubagentNotificationRun(parentKey, notification string) {
	params := RunParams{
		SessionKey:  parentKey,
		Message:     notification,
		ClientRunID: shortid.New("subnotify"),
		Delivery:    deliveryFromSessionKey(parentKey),
	}

	// Double-check: if a run started between the flush decision and here,
	// safely enqueue instead of starting a second concurrent run.
	if h.hasActiveRunForSession(parentKey) {
		h.enqueuePending(parentKey, params)
		return
	}

	h.startAsyncRun("subnotify", params, false)
}

// buildNotifyItem extracts the relevant fields from a completed child session.
func buildNotifyItem(child *session.Session) notifyItem {
	item := notifyItem{
		childKey:      child.Key,
		label:         child.Label,
		status:        child.Status,
		failureReason: child.FailureReason,
		lastOutput:    child.LastOutput,
	}
	if item.label == "" {
		item.label = abbreviateSession(child.Key)
	}
	if child.RuntimeMs != nil {
		item.runtimeMs = *child.RuntimeMs
	}
	return item
}

// formatBatchNotification renders a batch of child completions into a single
// structured notification. When the batch exceeds notifyQueueCap, overflowed
// items are summarized as a count to prevent unbounded text growth.
func formatBatchNotification(items []notifyItem) string {
	var sb strings.Builder

	// IMPORTANT: the trailing instruction prevents the LLM from using NO_REPLY
	// (which would suppress delivery of the synthesized response to the user).
	if len(items) == 1 {
		sb.WriteString("**System:** subagent completed. Synthesize the result below into your response for the user. Do NOT re-do this work. Do NOT use NO_REPLY — the user is waiting for this answer.\n")
		writeNotifyItem(&sb, items[0])
		return sb.String()
	}

	fmt.Fprintf(&sb, "**System:** %d subagents completed. Synthesize the results below into a unified response for the user. Do NOT re-do their work. Do NOT use NO_REPLY — the user is waiting for this answer.\n", len(items))

	// Render up to cap detailed items.
	rendered := items
	var overflowCount int
	if len(items) > notifyQueueCap {
		rendered = items[:notifyQueueCap]
		overflowCount = len(items) - notifyQueueCap
	}

	for i, item := range rendered {
		fmt.Fprintf(&sb, "\n### Agent %d/%d\n", i+1, len(items))
		writeNotifyItem(&sb, item)
	}

	if overflowCount > 0 {
		fmt.Fprintf(&sb, "\n... and %d more (use subagents tool for details)\n", overflowCount)
	}

	return sb.String()
}

// writeNotifyItem writes a single child's notification details.
func writeNotifyItem(sb *strings.Builder, item notifyItem) {
	fmt.Fprintf(sb, "- Agent: %s\n", item.label)
	fmt.Fprintf(sb, "- Status: %s\n", item.status)

	if item.runtimeMs > 0 {
		d := time.Duration(item.runtimeMs) * time.Millisecond
		fmt.Fprintf(sb, "- Runtime: %s\n", d.Round(time.Second))
	}

	if item.failureReason != "" {
		fmt.Fprintf(sb, "- Failure: %s\n", item.failureReason)
	}

	if item.lastOutput != "" {
		output := item.lastOutput
		const maxOutputLen = 2000
		if len(output) > maxOutputLen {
			output = output[:maxOutputLen] + "\n... (truncated)"
		}
		fmt.Fprintf(sb, "- Result:\n%s\n", output)
	}
}

// isTerminalStatus returns true for session statuses that represent completed runs.
func isTerminalStatus(s session.RunStatus) bool {
	switch s {
	case session.StatusDone, session.StatusFailed, session.StatusKilled, session.StatusTimeout:
		return true
	default:
		return false
	}
}
