package mailtool

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

// countingWikiAdapter is a knowledge.Adapter that returns one related hit and
// records how many recalls it served, so a test can assert the enrichment cap.
type countingWikiAdapter struct{ calls atomic.Int64 }

func (c *countingWikiAdapter) Layer() knowledge.Layer { return knowledge.LayerWiki }

func (c *countingWikiAdapter) Recall(_ context.Context, _ string, _ int) ([]knowledge.Result, error) {
	c.calls.Add(1)
	return []knowledge.Result{{Ref: knowledge.Ref{Layer: knowledge.LayerWiki, ID: "wiki/related"}, Snippet: "related", Score: 1}}, nil
}

func (c *countingWikiAdapter) Read(context.Context, string) (*knowledge.Document, error) {
	return nil, nil
}

// enrichArchiveMessages fans per-message wiki recall out under a bounded pool and
// only decorates the head of the list — each hit is a semantic recall, so a long
// list run sequentially made mail_archive latency track recall × N. This locks in
// order preservation, the top-K cap, and that no recall fires past the cap. Run
// with -race to catch parallel index corruption.
func TestEnrichArchiveMessagesCapsAndPreservesOrder(t *testing.T) {
	adapter := &countingWikiAdapter{}
	deps := MailArchiveDeps{Wiki: adapter}

	n := maxEnrichedMessages + 5
	msgs := make([]mailarchive.ContextMessage, n)
	for i := range msgs {
		msgs[i] = mailarchive.ContextMessage{ID: fmt.Sprintf("m%02d", i), Subject: fmt.Sprintf("subject %d", i)}
	}

	out := enrichArchiveMessages(context.Background(), deps, msgs, true)

	if len(out) != n {
		t.Fatalf("len(out) = %d, want %d", len(out), n)
	}
	for i := range out {
		if out[i].ID != msgs[i].ID {
			t.Fatalf("order corrupted at %d: got %q want %q", i, out[i].ID, msgs[i].ID)
		}
	}
	for i := 0; i < maxEnrichedMessages; i++ {
		if len(out[i].RelatedWiki) == 0 {
			t.Errorf("message %d within cap should be enriched with related wiki", i)
		}
	}
	for i := maxEnrichedMessages; i < n; i++ {
		if len(out[i].RelatedWiki) != 0 {
			t.Errorf("message %d beyond cap should be plain, got %d hits", i, len(out[i].RelatedWiki))
		}
	}
	if got := adapter.calls.Load(); got != int64(maxEnrichedMessages) {
		t.Errorf("recall calls = %d, want %d (one per enriched message, capped)", got, maxEnrichedMessages)
	}
}
