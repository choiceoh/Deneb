// Event publisher: enriches the broadcaster with session snapshot building,
// presence broadcasting, and coordinated event delivery.
//
// Originally ported from the retired TypeScript gateway event-subscription
// and presence publishers.
package events

import (
	"log/slog"
	"sync"
	"time"
)

// SessionSnapshot holds a point-in-time session state for broadcast payloads.
type SessionSnapshot struct {
	SessionKey       string   `json:"sessionKey"`
	SessionID        string   `json:"sessionId,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	Channel          string   `json:"channel,omitempty"`
	Label            string   `json:"label,omitempty"`
	DisplayName      string   `json:"displayName,omitempty"`
	ParentSessionKey string   `json:"parentSessionKey,omitempty"`
	Status           string   `json:"status,omitempty"`
	Model            string   `json:"model,omitempty"`
	ModelProvider    string   `json:"modelProvider,omitempty"`
	StartedAt        *int64   `json:"startedAt,omitempty"`
	EndedAt          *int64   `json:"endedAt,omitempty"`
	RuntimeMs        *int64   `json:"runtimeMs,omitempty"`
	TotalTokens      *int64   `json:"totalTokens,omitempty"`
	EstimatedCostUsd *float64 `json:"estimatedCostUsd,omitempty"`
	AbortedLastRun   bool     `json:"abortedLastRun,omitempty"`
}

// SessionSnapshotProvider resolves session snapshots for event enrichment.
type SessionSnapshotProvider interface {
	SessionSnapshot(sessionKey string) *SessionSnapshot
}

// Publisher enriches the broadcaster with session-aware event delivery.
type Publisher struct {
	broadcaster *Broadcaster
	snapshots   SessionSnapshotProvider
	logger      *slog.Logger

	// Agent run event sequencing.
	seqMu    sync.Mutex
	agentSeq map[string]uint64 // runId -> sequence number
}

// NewPublisher creates a new event publisher wrapping the given broadcaster.
func NewPublisher(b *Broadcaster, snapshots SessionSnapshotProvider, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		broadcaster: b,
		snapshots:   snapshots,
		logger:      logger,
		agentSeq:    make(map[string]uint64),
	}
}

// PublishSessionMessage publishes a session message event to subscribers.
// Sends "session.message" to session subscribers and "sessions.changed" to
// session event subscribers.
func (p *Publisher) PublishSessionMessage(update TranscriptUpdate) {
	if p == nil || p.broadcaster == nil || update.SessionKey == "" || update.Message == nil {
		return
	}

	connIDs := p.broadcaster.MergedSessionRecipients(update.SessionKey)
	if len(connIDs) == 0 {
		return
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

	// Enrich with session snapshot.
	if p.snapshots != nil {
		if snap := p.snapshots.SessionSnapshot(update.SessionKey); snap != nil {
			payload["session"] = snap
		}
	}

	p.broadcaster.BroadcastToConnIDs("session.message", payload, connIDs)

	// Also notify session event subscribers.
	p.publishSessionChanged(update.SessionKey, "message", nil)
}

// PublishAgentEvent publishes an agent event with sequencing.
func (p *Publisher) PublishAgentEvent(evt AgentEvent) {
	if p == nil || p.broadcaster == nil {
		return
	}
	payload := map[string]any{
		"kind": evt.Kind,
	}
	if evt.SessionKey != "" {
		payload["sessionKey"] = evt.SessionKey
	}
	if evt.RunID != "" {
		payload["runId"] = evt.RunID

		// Add sequence number.
		p.seqMu.Lock()
		p.agentSeq[evt.RunID]++
		payload["seq"] = p.agentSeq[evt.RunID]
		p.seqMu.Unlock()
	}
	if evt.Payload != nil {
		payload["payload"] = evt.Payload
	}

	// Target session subscribers if sessionKey is present.
	if evt.SessionKey != "" {
		connIDs := p.broadcaster.MergedSessionRecipients(evt.SessionKey)
		if len(connIDs) == 0 {
			return
		}
		p.broadcaster.BroadcastWithOpts("agent.event", payload, BroadcastOpts{
			DropIfSlow:    true,
			TargetConnIDs: connIDs,
		})
		return
	}

	p.broadcaster.BroadcastWithOpts("agent.event", payload, BroadcastOpts{DropIfSlow: true})
}

// PublishConfigChanged broadcasts a configuration change event.
func (p *Publisher) PublishConfigChanged(section string) {
	if p == nil || p.broadcaster == nil {
		return
	}
	p.broadcaster.BroadcastWithOpts("config.changed", map[string]any{
		"section": section,
		"ts":      time.Now().UnixMilli(),
	}, BroadcastOpts{})
}

// CleanupAgentSeq removes sequence tracking for a completed agent run.
func (p *Publisher) CleanupAgentSeq(runID string) {
	if p == nil {
		return
	}
	p.seqMu.Lock()
	delete(p.agentSeq, runID)
	p.seqMu.Unlock()
}

// publishSessionChanged sends a sessions.changed event to session event subscribers.
func (p *Publisher) publishSessionChanged(sessionKey, phase string, overrides map[string]any) {
	if p == nil || p.broadcaster == nil {
		return
	}
	connIDs := p.broadcaster.SessionEventSubscriberConnIDs()
	if len(connIDs) == 0 {
		return
	}

	payload := map[string]any{
		"sessionKey": sessionKey,
		"ts":         time.Now().UnixMilli(),
	}
	if phase != "" {
		payload["phase"] = phase
	}

	// Enrich with session snapshot.
	if p.snapshots != nil {
		if snap := p.snapshots.SessionSnapshot(sessionKey); snap != nil {
			payload["session"] = snap
		}
	}

	// Apply overrides.
	for k, v := range overrides {
		payload[k] = v
	}

	p.broadcaster.BroadcastWithOpts("sessions.changed", payload, BroadcastOpts{
		DropIfSlow:    true,
		TargetConnIDs: connIDs,
	})
}
