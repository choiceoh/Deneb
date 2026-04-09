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
	"log/slog"
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

// enqueue adds a notification and resets the debounce timer.
func (q *notifyQueue) enqueue(item notifyItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
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

// SubagentNotifier manages child session completion notifications to parent sessions.
// Thread-safe. Routes notifications via DeferredSystemText (parent running) or
// triggers a new run (parent idle).
type SubagentNotifier struct {
	mu       sync.Mutex
	channels map[string]chan string  // parent sessionKey → buffered notification channel
	queues   map[string]*notifyQueue // parent sessionKey → debounced queue
	logger   *slog.Logger

	// Injected dependencies (avoid circular Handler reference).
	hasActiveRun func(sessionKey string) bool
	startRun     func(reqID string, params RunParams, isSteer bool)
	enqueuePend  func(sessionKey string, params RunParams)
	getSessions  func() *session.Manager
}

// SubagentNotifierDeps holds the dependencies for SubagentNotifier.
type SubagentNotifierDeps struct {
	Logger       *slog.Logger
	HasActiveRun func(sessionKey string) bool
	StartRun     func(reqID string, params RunParams, isSteer bool)
	EnqueuePend  func(sessionKey string, params RunParams)
	Sessions     func() *session.Manager
}

// NewSubagentNotifier creates a SubagentNotifier and subscribes to session events.
func NewSubagentNotifier(deps SubagentNotifierDeps) *SubagentNotifier {
	sn := &SubagentNotifier{
		channels:     make(map[string]chan string),
		queues:       make(map[string]*notifyQueue),
		logger:       deps.Logger,
		hasActiveRun: deps.HasActiveRun,
		startRun:     deps.StartRun,
		enqueuePend:  deps.EnqueuePend,
		getSessions:  deps.Sessions,
	}
	if sn.logger == nil {
		sn.logger = slog.Default()
	}

	sm := sn.getSessions()
	bus := sm.EventBusRef()
	bus.Subscribe(func(event session.Event) {
		if event.Kind != session.EventStatusChanged {
			return
		}
		if !isTerminalStatus(event.NewStatus) {
			return
		}

		child := sm.Get(event.Key)
		if child == nil || child.SpawnedBy == "" {
			return
		}

		parentKey := child.SpawnedBy
		item := buildNotifyItem(child)

		q := sn.getOrCreateQueue(parentKey)
		q.enqueue(item)

		sn.logger.Info("subagent completion queued for parent",
			"child", abbreviateSession(event.Key),
			"parent", abbreviateSession(parentKey),
			"status", string(event.NewStatus))
	})

	return sn
}

// NotifyCh returns a read-only view of the notification channel for a parent
// session, or nil if none exists. Used by DeferredSystemText composition.
func (sn *SubagentNotifier) NotifyCh(sessionKey string) <-chan string {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	ch := sn.channels[sessionKey]
	if ch == nil {
		return nil
	}
	return ch
}

// Reset clears all notification state.
func (sn *SubagentNotifier) Reset() {
	sn.mu.Lock()
	sn.channels = make(map[string]chan string)
	sn.queues = make(map[string]*notifyQueue)
	sn.mu.Unlock()
}

// getOrCreateQueue returns the debounced queue for a parent, creating lazily.
func (sn *SubagentNotifier) getOrCreateQueue(parentKey string) *notifyQueue {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	if q, ok := sn.queues[parentKey]; ok {
		return q
	}

	q := &notifyQueue{
		capacity: notifyQueueCap,
		flushFn: func(items []notifyItem) {
			notification := formatBatchNotification(items)

			if sn.hasActiveRun(parentKey) {
				sn.pushNotification(parentKey, notification)
			} else {
				sn.triggerRun(parentKey, notification)
			}

			sn.logger.Info("subagent batch notification flushed",
				"parent", abbreviateSession(parentKey),
				"count", len(items),
				"parentRunning", sn.hasActiveRun(parentKey))
		},
	}
	sn.queues[parentKey] = q
	return q
}

// getOrCreateCh returns the write end of the notification channel, creating lazily.
func (sn *SubagentNotifier) getOrCreateCh(sessionKey string) chan<- string {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	ch, ok := sn.channels[sessionKey]
	if !ok {
		ch = make(chan string, subagentNotifyChCap)
		sn.channels[sessionKey] = ch
	}
	return ch
}

// pushNotification sends a notification to the parent's channel (non-blocking).
func (sn *SubagentNotifier) pushNotification(parentKey, notification string) {
	ch := sn.getOrCreateCh(parentKey)
	select {
	case ch <- notification:
	default:
		sn.logger.Warn("subagent notification channel full, dropping",
			"parent", abbreviateSession(parentKey))
	}
}

// triggerRun starts a run for an idle parent session.
func (sn *SubagentNotifier) triggerRun(parentKey, notification string) {
	params := RunParams{
		SessionKey:  parentKey,
		Message:     notification,
		ClientRunID: shortid.New("subnotify"),
		Delivery:    deliveryFromSessionKey(parentKey),
	}

	if sn.hasActiveRun(parentKey) {
		sn.enqueuePend(parentKey, params)
		return
	}

	sn.startRun("subnotify", params, false)
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
