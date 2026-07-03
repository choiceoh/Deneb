// workfeed_dream.go — surfaces a completed wiki dream cycle in the native
// work feed. The dreamer always wrote a proposal JSON to disk, but nothing
// user-facing showed that overnight consolidation happened; a compact card
// ("위키 드림: N 생성 · M 갱신") makes the autonomous work observable without
// a push notification (over-notification 금지 — the feed is pull).
package server

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// postDreamWorkfeedCard appends one feed card for a dream cycle that changed
// pages. No-change cycles post nothing — a card per idle 8h cycle would be
// noise. Nil-safe on every dependency (report, feed store).
func (s *Server) postDreamWorkfeedCard(r *autonomous.DreamReport) {
	if r == nil || r.WikiPagesCreated+r.WikiPagesUpdated == 0 {
		return
	}
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return
	}
	title := fmt.Sprintf("위키 드림: %d 생성 · %d 갱신", r.WikiPagesCreated, r.WikiPagesUpdated)
	summary := "일지·메모 통합 사이클이 위키를 갱신했습니다."
	if r.WikiUpdatesProposed > r.WikiPagesCreated+r.WikiPagesUpdated {
		summary = fmt.Sprintf("일지·메모 통합 — 제안 %d건 중 %d건 반영.",
			r.WikiUpdatesProposed, r.WikiPagesCreated+r.WikiPagesUpdated)
	}
	if _, err := feed.Append(workfeed.Item{
		Source:  workfeed.SourceDream,
		Title:   title,
		Summary: summary,
	}); err != nil {
		s.logger.Warn("dream workfeed card append failed", "error", err)
	}
}
