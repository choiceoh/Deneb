package mailtool

import (
	"context"
	"fmt"
	"strings"
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

type personConflictAdapter struct{}

func (personConflictAdapter) Layer() knowledge.Layer { return knowledge.LayerWiki }

func (personConflictAdapter) Recall(_ context.Context, _ string, _ int) ([]knowledge.Result, error) {
	return []knowledge.Result{{
		Ref:     knowledge.Ref{Layer: knowledge.LayerWiki, ID: "인물/이현성.md"},
		Snippet: "기아 광주시설관리팀",
		Score:   1,
		Meta:    map[string]string{"emails": "2555151@kia.com"},
	}}, nil
}

func (personConflictAdapter) Read(context.Context, string) (*knowledge.Document, error) {
	return nil, nil
}

func TestRelatedArchiveWikiFlagsPersonConflictWithoutDroppingHit(t *testing.T) {
	deps := MailArchiveDeps{Wiki: personConflictAdapter{}}
	msg := mailarchive.ContextMessage{
		From:    "이현성 <lee@other.co.kr>",
		Subject: "광주 2공장 담당",
	}
	out := enrichArchiveMessage(context.Background(), deps, msg, true)
	if len(out.RelatedWiki) != 1 {
		t.Fatalf("related wiki = %d, want 1 (hit must stay)", len(out.RelatedWiki))
	}
	if out.RelatedWiki[0].Conflict == "" || !strings.Contains(out.RelatedWiki[0].Conflict, "불일치") {
		t.Fatalf("conflict = %q, want 불일치", out.RelatedWiki[0].Conflict)
	}

	agree := mailarchive.ContextMessage{
		From:    "이현성 <2555151@kia.com>",
		Subject: "광주 2공장 담당",
	}
	ok := enrichArchiveMessage(context.Background(), deps, agree, true)
	if len(ok.RelatedWiki) != 1 || ok.RelatedWiki[0].Conflict != "" {
		t.Fatalf("same address must not flag: %+v", ok.RelatedWiki)
	}
}

func TestArchiveWikiMailConflict_SameDomainAndNonPerson(t *testing.T) {
	meta := map[string]string{"emails": "2555151@kia.com"}
	if got := archiveWikiMailConflict("인물/이현성.md", meta, "", "jaeminkim@kia.com"); got != "" {
		t.Fatalf("same domain: %q", got)
	}
	if got := archiveWikiMailConflict("프로젝트/x/대표.md", meta, "", "lee@other.co.kr"); got != "" {
		t.Fatalf("non-person: %q", got)
	}
}
