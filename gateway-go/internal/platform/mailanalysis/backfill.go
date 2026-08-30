// backfill.go — analysis of mail that never came through a poll cycle.
//
// The poll query is `is:unread newer_than:1h` and Run() skips outside weekday
// business hours, so a mail that arrives at 20:00 has left the window before
// the next poll looks. service.go already names the consequence — "a wrongly-
// marked mail leaves the newer_than:1h window and is lost forever" — but only
// as a caution about marking seen; nothing recovered the ones that were lost.
//
// This is the recovery path. It takes messages the caller assembled from the
// local archive rather than from Gmail, and runs them through the SAME analysis
// and the SAME OnAnalyzed persistence as a poll, so every downstream guard
// (AnalysisUsable, AnalysisNonBusiness, the AutoFlow dedup) applies unchanged.
package mailanalysis

import (
	"context"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// AnalyzeArchived analyzes messages sourced from the local archive and persists
// each through OnAnalyzed. It returns the ids that produced an analysis.
//
// No Gmail client is passed: these messages already carry their bodies, and the
// LMTP ingest path established that pipelineDeps tolerates a nil client (the
// attachment fetcher is simply absent). Attachments that were never mirrored
// locally stay unread — an analysis without them is worth more than no analysis,
// which is what the mail has had until now.
func (s *Service) AnalyzeArchived(ctx context.Context, msgs []*gmail.MessageDetail) ([]string, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	report, items, err := AnalyzeBatch(ctx, s.pipelineDeps(nil), msgs)
	if err != nil && len(items) == 0 {
		return nil, fmt.Errorf("백필 분석 실패: %w", err)
	}
	_ = report // The consolidated report is a poll-cycle artifact; backfill is silent.
	s.persistPollAnalyses(items)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if it.Msg != nil {
			ids = append(ids, it.Msg.ID)
		}
	}
	return ids, nil
}
