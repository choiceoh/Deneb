// Gateway event subscriptions: wires session lifecycle, heartbeat, agent, and
// transcript events to the broadcaster for delivery to WebSocket clients.
//
// Mirrors createGatewayEventSubscriptions from
// src/gateway/server-event-subscriptions.ts.
package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// AgentEvent represents an agent bus event (agent run lifecycle, tool use, etc.).
type AgentEvent struct {
	Kind       string `json:"kind"`
	SessionKey string `json:"sessionKey,omitempty"`
	RunID      string `json:"runId,omitempty"`
	Payload    any    `json:"payload,omitempty"`
}

// HeartbeatEvent represents a periodic heartbeat from the agent runtime.
type HeartbeatEvent struct {
	SessionKey string `json:"sessionKey,omitempty"`
	Ts         int64  `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
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
	heartbeatCh  chan HeartbeatEvent
	transcriptCh chan TranscriptUpdate
	lifecycleCh  chan LifecycleChangeEvent
	done         chan struct{}

	// Optional publisher for enriched event delivery (set after construction).
	publisher unsafe.Pointer // *Publisher, accessed atomically

	// Drop counters for observability (atomic, no lock needed).
	agentDrops      atomic.Int64
	heartbeatDrops  atomic.Int64
	transcriptDrops atomic.Int64
	lifecycleDrops  atomic.Int64
}

// GatewaySubscriptionParams provides dependencies for event subscription wiring.
type GatewaySubscriptionParams struct {
	Broadcaster *Broadcaster
	Logger      *slog.Logger
}

// NewGatewayEventSubscriptions creates and starts event subscription goroutines
// that relay events from internal buses to WebSocket clients via the broadcaster.
func NewGatewayEventSubscriptions(params GatewaySubscriptionParams) *GatewayEventSubscriptions {
	g := &GatewayEventSubscriptions{
		agentCh:      make(chan AgentEvent, 256),
		heartbeatCh:  make(chan HeartbeatEvent, 64),
		transcriptCh: make(chan TranscriptUpdate, 256),
		lifecycleCh:  make(chan LifecycleChangeEvent, 64),
		done:         make(chan struct{}),
	}

	go g.runAgentLoop(params)
	go g.runHeartbeatLoop(params)
	go g.runTranscriptLoop(params)
	go g.runLifecycleLoop(params)
	go g.runDropLogger(params.Logger)

	return g
}

// SetPublisher sets the enrichment publisher for transcript and agent events.
// Safe to call after construction; the running loops pick it up atomically.
func (g *GatewayEventSubscriptions) SetPublisher(p *Publisher) {
	atomic.StorePointer(&g.publisher, unsafe.Pointer(p))
}

// getPublisher returns the current publisher, or nil if none is set.
func (g *GatewayEventSubscriptions) getPublisher() *Publisher {
	return (*Publisher)(atomic.LoadPointer(&g.publisher))
}

// EmitAgent sends an agent event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitAgent(evt AgentEvent) {
	select {
	case g.agentCh <- evt:
	default:
		g.agentDrops.Add(1)
	}
}

// EmitHeartbeat sends a heartbeat event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitHeartbeat(evt HeartbeatEvent) {
	select {
	case g.heartbeatCh <- evt:
	default:
		g.heartbeatDrops.Add(1)
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
			params.Broadcaster.BroadcastWithOpts("agent", evt, BroadcastOpts{DropIfSlow: true})
		}
	}
}

func (g *GatewayEventSubscriptions) runHeartbeatLoop(params GatewaySubscriptionParams) {
	for {
		select {
		case <-g.done:
			return
		case evt := <-g.heartbeatCh:
			params.Broadcaster.BroadcastWithOpts("heartbeat", evt, BroadcastOpts{DropIfSlow: true})
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

			params.Broadcaster.BroadcastToConnIDs("session.message", payload, connIDs)

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
				params.Broadcaster.BroadcastToConnIDs("sessions.changed", changedPayload, sessionEventConnIDs)
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

			params.Broadcaster.BroadcastToConnIDs("sessions.changed", payload, connIDs)
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
			heartbeat := g.heartbeatDrops.Swap(0)
			transcript := g.transcriptDrops.Swap(0)
			lifecycle := g.lifecycleDrops.Swap(0)
			total := agent + heartbeat + transcript + lifecycle
			if total > 0 {
				logger.Warn("gateway event subscriptions dropped events",
					"agent", agent,
					"heartbeat", heartbeat,
					"transcript", transcript,
					"lifecycle", lifecycle,
				)
			}
		}
	}
}
