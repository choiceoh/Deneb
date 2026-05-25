package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/hindsight"
)

// hindsightAdapter exposes the Vectorize Hindsight memory bank as a
// knowledge backend. Read is currently not supported because the client does
// not expose a "fetch by id" endpoint — memories are reached through Recall.
type hindsightAdapter struct {
	client *hindsight.Client
}

// NewHindsightAdapter wraps a configured Hindsight client. Returns nil when
// the client is nil or disabled so Router skips the backend silently.
func NewHindsightAdapter(client *hindsight.Client) Adapter {
	if client == nil {
		return nil
	}
	return &hindsightAdapter{client: client}
}

func (a *hindsightAdapter) Layer() Layer { return LayerHindsight }

func (a *hindsightAdapter) Recall(ctx context.Context, query string, limit int) ([]Result, error) {
	memories, err := a.client.Recall(ctx, query)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(memories) > limit {
		memories = memories[:limit]
	}
	out := make([]Result, 0, len(memories))
	// Hindsight returns results already ranked; surface that ordering as a
	// descending score so the router merge keeps the relative position.
	for i, m := range memories {
		score := 1.0 - float64(i)*0.01
		if score < 0 {
			score = 0
		}
		out = append(out, Result{
			Ref:     Ref{Layer: LayerHindsight, ID: m.ID},
			Snippet: m.Text,
			Score:   score,
			Time:    parseISOTime(m.OccurredAt),
		})
	}
	return out, nil
}

// Read is intentionally a soft failure for hindsight — the underlying client
// does not expose a fetch-by-id endpoint. The router still answers, telling
// the agent to use recall instead.
func (a *hindsightAdapter) Read(_ context.Context, id string) (*Document, error) {
	return nil, fmt.Errorf("hindsight does not support direct read of id %q; use knowledge(op=\"recall\", query=...) instead", id)
}

// parseISOTime accepts ISO 8601 and returns unix milli. 0 on failure.
func parseISOTime(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}
