// Gateway event subscriptions: wires session lifecycle, heartbeat, agent, and
// transcript events to the broadcaster for delivery to WebSocket clients.
//
// Mirrors createGatewayEventSubscriptions from
// src/gateway/server-event-subscriptions.ts.
package events

import (
	"log/slog"
	"sync"
	"time"
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
	Ts         int64  `json:"ts"`
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

	return g
}

// EmitAgent sends an agent event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitAgent(evt AgentEvent) {
	select {
	case g.agentCh <- evt:
	default:
	}
}

// EmitHeartbeat sends a heartbeat event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitHeartbeat(evt HeartbeatEvent) {
	select {
	case g.heartbeatCh <- evt:
	default:
	}
}

// EmitTranscript sends a transcript update into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitTranscript(evt TranscriptUpdate) {
	select {
	case g.transcriptCh <- evt:
	default:
	}
}

// EmitLifecycle sends a lifecycle change event into the subscription pipeline.
func (g *GatewayEventSubscriptions) EmitLifecycle(evt LifecycleChangeEvent) {
	select {
	case g.lifecycleCh <- evt:
	default:
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

			sessionEventConnIDs := params.Broadcaster.GetSessionEventSubscriberConnIDs()
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
			connIDs := params.Broadcaster.GetSessionEventSubscriberConnIDs()
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
