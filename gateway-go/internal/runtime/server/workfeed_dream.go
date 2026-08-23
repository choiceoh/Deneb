// workfeed_dream.go — surfaces a completed wiki dream cycle in the native
// work feed. The dreamer always wrote a proposal JSON to disk, but nothing
// user-facing showed that overnight consolidation happened; a compact card
// ("위키 드림: N 생성 · M 갱신") makes the autonomous work observable without
// a push notification (over-notification 금지 — the feed is pull).
package server

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// postDreamWorkfeedCard appends one feed card for a dream cycle that changed
// pages — synthesis creates/updates or project digest refreshes (Phase 3d
// writes 대표페이지 "## 현재 상태" sections outside the apply counters). No-change
// cycles post nothing — a card per idle 8h cycle would be noise. Nil-safe on
// every dependency (report, feed store).
func (s *Server) postDreamWorkfeedCard(r *autonomous.DreamReport) {
	if r == nil || r.WikiPagesCreated+r.WikiPagesUpdated+r.WikiProjectDigests == 0 {
		return
	}
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return
	}
	applied := r.WikiPagesCreated + r.WikiPagesUpdated
	title := fmt.Sprintf("위키 드림: %d 생성 · %d 갱신", r.WikiPagesCreated, r.WikiPagesUpdated)
	switch {
	case applied == 0:
		title = fmt.Sprintf("위키 드림: 프로젝트 근황 %d건 갱신", r.WikiProjectDigests)
	case r.WikiProjectDigests > 0:
		title += fmt.Sprintf(" · 근황 %d", r.WikiProjectDigests)
	}
	summary := "일지·메모 통합 사이클이 위키를 갱신했습니다."
	if r.WikiUpdatesProposed > applied {
		summary = fmt.Sprintf("일지·메모 통합 — 제안 %d건 중 %d건 반영.",
			r.WikiUpdatesProposed, applied)
	}
	// Surface the cycle's self-graded quality (0 = unscored) so the operator can
	// trend it without opening the proposal JSON — volume is already in the title,
	// this is the OUTPUT signal (precision·confidence·recall utility).
	if r.QualityScore > 0 {
		summary += fmt.Sprintf(" 품질 %d/100", int(r.QualityScore+0.5))
		if r.RecallHitPages > 0 {
			summary += fmt.Sprintf(" · 회상활용 %d면", r.RecallHitPages)
		}
	}
	if _, err := feed.Append(workfeed.Item{
		Source:  workfeed.SourceDream,
		Title:   title,
		Summary: summary,
		// Trust Inbox (improvement-ideas.md 4.7): the dream card is settleable
		// like every other autonomous-change card. A machine-rollback action is
		// deliberately absent — the dreamer's change summary is prose, not a
		// restorable snapshot; operator correction goes through the weekly
		// memory digest feedback (4.9) or a chat turn.
		Actions: []workfeed.Action{{ID: "dream:ack", Kind: workfeed.ActionAck, Label: "확인"}},
	}); err != nil {
		s.logger.Warn("dream workfeed card append failed", "error", err)
	}
}
