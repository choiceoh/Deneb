package server

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// deadlineDoneActionPrefix marks a morning-card deadline "완료" long-press.
// The suffix is the wiki page's relative path (the deadline scan's Path).
const deadlineDoneActionPrefix = "deadline_done:"

// markDeadlineDone stamps a wiki page's due_done with its current due when the
// operator long-presses that deadline's row in the morning-letter feed card.
// The deadline scan then skips the page until a NEW due is set, so a handled
// deadline stops nagging without losing the date (history preserved).
// Best-effort, fire-and-forget (the workfeed action already succeeded).
func (s *Server) markDeadlineDone(_ workfeed.Item, actionID string) {
	if s.wikiStore == nil {
		return
	}
	pagePath := strings.TrimSpace(strings.TrimPrefix(actionID, deadlineDoneActionPrefix))
	if pagePath == "" {
		return
	}
	page, err := s.wikiStore.ReadPage(pagePath)
	if err != nil || page == nil {
		s.logger.Warn("deadline done: 페이지 읽기 실패", "path", pagePath, "error", err)
		return
	}
	if strings.TrimSpace(page.Meta.Due) == "" {
		// No live deadline (already cleared/changed) — nothing to stamp.
		s.logger.Info("deadline done: due 없음, 스킵", "path", pagePath)
		return
	}
	page.Meta.DueDone = page.Meta.Due
	if strings.TrimSpace(page.Meta.Updated) == "" || page.Meta.Updated < page.Meta.Due {
		page.Meta.Updated = time.Now().Format("2006-01-02")
	}
	if err := s.wikiStore.WritePage(pagePath, page); err != nil {
		s.logger.Warn("deadline done 기록 실패", "path", pagePath, "error", err)
		return
	}
	s.logger.Info("deadline done: due_done 스탬프", "path", pagePath, "due", page.Meta.Due)
}
