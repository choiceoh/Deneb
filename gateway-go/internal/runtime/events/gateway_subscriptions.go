// Gateway event subscriptions: wires session lifecycle, agent, and transcript
// events to the broadcaster for delivery to SSE clients.
//
// Mirrors createGatewayEventSubscriptions from
// the retired TypeScript gateway event-subscriptions module.
package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// AgentEvent represents an agent bus event (agent run lifecycle, tool use, etc.).
type AgentEvent struct {
	Kind       string `json:"kind"`
	SessionKey string `json:"sessionKey,omitempty"`
	RunID      string `json:"runId,omitempty"`
	Payload    any    `json:"payload,omitempty"`
}

// TranscriptUpdate represents a session transcript message update.
type TranscriptUpdate struct {
	SessionKey string `json:"sessionKey,omitempty"`
	MessageID  string `json:"messageId,omitempty"`
	MessageSeq *int   `json:"messageSeq,omitempty"`
	Message    any    `json:"message,omitempty"`
}

// LifecycleChangeEvent represents a session lifecycle state change.
type LifecycleChangeEvent struct {
	SessionKey       string `json:"sessionKey"`
	Reason           string `json:"reason,omitempty"`
	ParentSessionKey string `json:"parentSessionKey,omitempty"`
	Label            string `json:"label,omitempty"`
	DisplayName      string `json:"displayName,omitempty"`
}

// GatewayEventSubscriptions manages event source subscriptions and their cleanup.
type GatewayEventSubscriptions struct {
	mu      sync.Mutex
	stopped bool

	agentCh      chan AgentEvent
	transcriptCh chan TranscriptUpdate
	lifecycleCh  chan LifecycleChangeEvent
	done         chan struct{}

	// Optional publisher for enriched event delivery (set after construction).
	publisher atomic.Pointer[Publisher]

	// Drop counters for observability (atomic, no lock needed).
	agentDrops      atomic.Int64
	transcriptDrops atomic.Int64
	lifecycleDrops  atomic.Int64
}

// GatewaySubscriptionParams provides dependencies for event subscription wiring.
type GatewaySubscriptionParams struct {
	Broadcaster *Broadcaster
	Logger      *slog.Logger
}

// NewGatewayEventSubscriptions creates and starts event subscription goroutines
// that relay events from internal buses to SSE clients via the broadcaster.
func NewGatewayEventSubscriptions(params GatewaySubscriptionParams) *GatewayEventSubscriptions {
	g := &GatewayEventSubscriptions{
		agentCh:      make(chan AgentEvent, 256),
		transcriptCh: make(chan TranscriptUpdate, 256),
		lifecycleCh:  make(chan LifecycleChangeEvent, 64),
		done:         make(chan struct{}),
	}

	safego.GoWithSlog(params.Logger, "gateway-event-agent", func() { g.runAgentLoop(params) })
	safego.GoWithSlog(params.Logger, "gateway-event-transcript", func() { g.runTranscriptLoop(params) })
	safego.GoWithSlog(params.Logger, "gateway-event-lifecycle", func() { g.runLifecycleLoop(params) })
	safego.GoWithSlog(params.Logger, "gateway-event-drop-logger", func() { g.runDropLogger(params.Logger) })

	return g
}

// SetPublisher sets the enrichment publisher for transcript and agent events.
// Safe to call after construction; the running loops pick it up atomically.
func (g *GatewayEventSubscriptions) SetPublisher(p *Publisher) {
	g.publisher.Store(p)
}

// getPublisher returns the current publisher, or nil if none is set.
func (g *GatewayEventSubscriptions) getPublisher() *Publisher {
	return g.publisher.Load()
}

// EmitAgent sends an agent event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitAgent(evt AgentEvent) {
	select {
	case g.agentCh <- evt:
	default:
		g.agentDrops.Add(1)
	}
}

// EmitTranscript sends a transcript update into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitTranscript(evt TranscriptUpdate) {
	select {
	case g.transcriptCh <- evt:
	default:
		g.transcriptDrops.Add(1)
	}
}

// EmitLifecycle sends a lifecycle change event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitLifecycle(evt LifecycleChangeEvent) {
	select {
	case g.lifecycleCh <- evt:
	default:
		g.lifecycleDrops.Add(1)
	}
}

// Stop shuts down all subscription goroutines.
func (g *GatewayEventSubscriptions) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	g.stopped = true
	close(g.done)
}

func (g *GatewayEventSubscriptions) runAgentLoop(params GatewaySubscriptionParams) {
	for {
		select {
		case <-g.done:
			return
		case evt := <-g.agentCh:
			// Delegate to publisher for sequenced delivery when available.
			if pub := g.getPublisher(); pub != nil {
				pub.PublishAgentEvent(evt)
				continue
			}
			// Fallback: direct broadcast without sequencing.
			agentWire, _ := PayloadOf(evt)
			params.Broadcaster.BroadcastWithOpts("agent", agentWire, BroadcastOpts{DropIfSlow: true})
		}
	}
}

func (g *GatewayEventSubscriptions) runTranscriptLoop(params GatewaySubscriptionParams) {
	for {
		select {
		case <-g.done:
			return
		case update := <-g.transcriptCh:
			// Delegate to publisher for enriched delivery when available.
			if pub := g.getPublisher(); pub != nil {
				pub.PublishSessionMessage(update)
				continue
			}

			// Fallback: direct broadcast without session snapshot enrichment.
			if update.SessionKey == "" || update.Message == nil {
				continue
			}

			connIDs := params.Broadcaster.MergedSessionRecipients(update.SessionKey)
			if len(connIDs) == 0 {
				continue
			}

			payload := map[string]any{
				"sessionKey": update.SessionKey,
				"message":    update.Message,
			}
			if update.MessageID != "" {
				payload["messageId"] = update.MessageID
			}
			if update.MessageSeq != nil {
				payload["messageSeq"] = *update.MessageSeq
			}

			msgWire, _ := PayloadOf(payload)
			params.Broadcaster.BroadcastToConnIDs("session.message", msgWire, connIDs)

			sessionEventConnIDs := params.Broadcaster.SessionEventSubscriberConnIDs()
			if len(sessionEventConnIDs) > 0 {
				changedPayload := map[string]any{
					"sessionKey": update.SessionKey,
					"phase":      "message",
					"ts":         time.Now().UnixMilli(),
				}
				if update.MessageID != "" {
					changedPayload["messageId"] = update.MessageID
				}
				if update.MessageSeq != nil {
					changedPayload["messageSeq"] = *update.MessageSeq
				}
				changedWire, _ := PayloadOf(changedPayload)
				params.Broadcaster.BroadcastToConnIDs("sessions.changed", changedWire, sessionEventConnIDs)
			}
		}
	}
}

func (g *GatewayEventSubscriptions) runLifecycleLoop(params GatewaySubscriptionParams) {
	for {
		select {
		case <-g.done:
			return
		case evt := <-g.lifecycleCh:
			connIDs := params.Broadcaster.SessionEventSubscriberConnIDs()
			if len(connIDs) == 0 {
				continue
			}

			payload := map[string]any{
				"sessionKey": evt.SessionKey,
				"ts":         time.Now().UnixMilli(),
			}
			if evt.Reason != "" {
				payload["reason"] = evt.Reason
			}
			if evt.ParentSessionKey != "" {
				payload["parentSessionKey"] = evt.ParentSessionKey
			}
			if evt.Label != "" {
				payload["label"] = evt.Label
			}
			if evt.DisplayName != "" {
				payload["displayName"] = evt.DisplayName
			}

			lifecycleWire, _ := PayloadOf(payload)
			params.Broadcaster.BroadcastToConnIDs("sessions.changed", lifecycleWire, connIDs)
		}
	}
}

// runDropLogger periodically logs dropped event counts for observability.
func (g *GatewayEventSubscriptions) runDropLogger(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-g.done:
			return
		case <-ticker.C:
			agent := g.agentDrops.Swap(0)
			transcript := g.transcriptDrops.Swap(0)
			lifecycle := g.lifecycleDrops.Swap(0)
			total := agent + transcript + lifecycle
			if total > 0 {
				logger.Warn(
					"gateway event subscriptions dropped events",
					"agent", agent,
					"transcript", transcript,
					"lifecycle", lifecycle,
				)
			}
		}
	}
}
