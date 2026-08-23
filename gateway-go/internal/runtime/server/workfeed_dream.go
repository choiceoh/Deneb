// workfeed_dream.go — surfaces a completed wiki dream cycle in the native
// work feed. The dreamer always wrote a proposal JSON to disk, but nothing
// user-facing showed that overnight consolidation happened; a compact card
// ("위키 드림: N 생성 · M 갱신") makes the autonomous work observable without
// a push notification (over-notification 금지 — the feed is pull).
package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
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
	// 5.7 loop closure: corrections the critique actually consumed this cycle —
	// the operator sees their earlier "틀린 기억" land, not vanish into the queue.
	if r.CorrectionsConsidered > 0 {
		summary += fmt.Sprintf(" · 정정 %d건 반영", r.CorrectionsConsidered)
	}
	// W15-style 정비 signal: verify advisories the operator should see (first-time
	// findings listed in the body, repeats only counted).
	maintCount := len(r.VerifyFindings) + r.VerifyFindingsRepeat
	if maintCount > 0 {
		summary += fmt.Sprintf(" · 정비 필요 %d건", maintCount)
	}

	body := strings.TrimSpace(title + "\n" + r.WikiChangeSummary)
	if maintCount > 0 {
		var b strings.Builder
		b.WriteString("\n\n## 정비 필요\n")
		for _, finding := range r.VerifyFindings {
			b.WriteString("- ")
			b.WriteString(finding)
			b.WriteByte('\n')
		}
		if r.VerifyFindingsRepeat > 0 {
			fmt.Fprintf(&b, "- 이전에 보고된 자문 %d건 포함\n", r.VerifyFindingsRepeat)
		}
		body += b.String()
	}
	if r.CorrectionsConsidered > 0 {
		body += fmt.Sprintf("\n이전 정정 %d건이 이번 비평에 반증 증거로 주입됐습니다.\n", r.CorrectionsConsidered)
	}

	// Trust Inbox (improvement-ideas.md 4.7 + 5.8): the dream card settles on
	// 확인 and on whole-cycle 되돌리기 (the card's work is done either way).
	// Per-page 되돌리기 rides MARK actions — the card survives individual
	// repairs, resolving each page from the cycle ledger at run time, so good
	// and bad changes in one commit no longer live or die together.
	actions := []workfeed.Action{{ID: "dream:ack", Kind: workfeed.ActionAck, Label: "확인"}}
	if r.GitCommit != "" {
		actions = append(actions, workfeed.Action{
			ID: dreamRevertActionPrefix + r.GitCommit, Kind: workfeed.ActionAck, Label: "전체 되돌리기",
		})
		pageActions, overflow := s.dreamPageRevertActions(r.GitCommit)
		actions = append(actions, pageActions...)
		if overflow > 0 {
			body += fmt.Sprintf("\n변경 페이지가 많아 페이지별 되돌리기는 %d건까지만 표시합니다 — 나머지는 전체 되돌리기로 함께 됩니다.\n", dreamMaxPageRevertActions)
		}
	}
	if _, err := feed.Append(workfeed.Item{
		Source:  workfeed.SourceDream,
		Title:   title,
		Summary: summary,
		RefType: "dream-cycle",
		RefID:   r.GitCommit,
		Body:    strings.TrimSpace(body),
		Actions: actions,
	}); err != nil {
		s.logger.Warn("dream workfeed card append failed", "error", err)
	}
}

// dreamRevertActionPrefix carries the cycle's git snapshot hash in the action
// id: "dream:revert:<hash>" (whole cycle) and "dream:revert-page:<hash>:<idx>"
// (a single page, index into wiki.DreamRevertPageList's canonical order).
const (
	dreamRevertActionPrefix     = "dream:revert:"
	dreamRevertPageActionPrefix = "dream:revert-page:"
	dreamMaxPageRevertActions   = 10
)

// dreamPageRevertActions mints one mark action per page the cycle touched. The
// index addresses the ledger's canonical page order, so the chip payload never
// inlines a path. Returns the actions and how many pages were past the cap.
func (s *Server) dreamPageRevertActions(hash string) ([]workfeed.Action, int) {
	if s.wikiStore == nil {
		return nil, 0
	}
	rec, ok := s.wikiStore.DreamCycleChangesByCommit(hash)
	if !ok {
		return nil, 0
	}
	pages := wiki.DreamRevertPageList(rec)
	if len(pages) < 2 {
		return nil, 0 // one page — the whole-cycle revert already covers it
	}
	ops := dreamPageOpLabels(rec)
	capped := pages
	overflow := 0
	if len(capped) > dreamMaxPageRevertActions {
		overflow = len(capped) - dreamMaxPageRevertActions
		capped = capped[:dreamMaxPageRevertActions]
	}
	out := make([]workfeed.Action, 0, len(capped))
	for i, page := range capped {
		out = append(out, workfeed.Action{
			ID:    fmt.Sprintf("%s%s:%d", dreamRevertPageActionPrefix, hash, i),
			Kind:  workfeed.ActionMark,
			Label: "↩ " + ops[page] + " · " + shortDreamPageLabel(page),
		})
	}
	return out, overflow
}

// dreamPageOpLabels maps page → the cycle's operation for chip labels; when a
// page appears in several lists the synthesis-side op (학습) wins.
func dreamPageOpLabels(rec wiki.DreamCycleChanges) map[string]string {
	ops := make(map[string]string, len(rec.Learned)+len(rec.Merged)+len(rec.Expired)+len(rec.Moved))
	for _, p := range rec.Expired {
		ops[p] = "보관"
	}
	for _, p := range rec.Moved {
		ops[p] = "이동"
	}
	for _, p := range rec.Merged {
		ops[p] = "병합"
	}
	for _, p := range rec.Learned {
		ops[p] = "학습"
	}
	return ops
}

// shortDreamPageLabel shortens a wiki path to its page name
// ("프로젝트/pl2-x/대표.md" → "대표").
func shortDreamPageLabel(page string) string {
	if idx := strings.LastIndexByte(page, '/'); idx >= 0 && idx+1 < len(page) {
		page = page[idx+1:]
	}
	return strings.TrimSuffix(page, ".md")
}

// handleDreamRevertCardAction reverts a dream cycle — the whole snapshot commit
// or a single page. An error keeps the card unsettled so the operator can
// retry (or investigate the conflict).
func (s *Server) handleDreamRevertCardAction(item workfeed.Item, actionID string) error {
	if s.wikiStore == nil {
		return errors.New("wiki store unavailable")
	}
	if strings.HasPrefix(actionID, dreamRevertPageActionPrefix) {
		return s.handleDreamRevertPageAction(item, actionID)
	}
	if !strings.HasPrefix(actionID, dreamRevertActionPrefix) {
		return fmt.Errorf("unsupported dream action %q", actionID)
	}
	hash := strings.TrimSpace(strings.TrimPrefix(actionID, dreamRevertActionPrefix))
	if hash == "" {
		hash = strings.TrimSpace(item.RefID)
	}
	if hash == "" {
		return errors.New("dream card carries no cycle hash")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.wikiStore.RevertDreamCycle(ctx, hash); err != nil {
		s.logger.Warn("dream cycle revert failed", "commit", hash, "error", err)
		return fmt.Errorf("revert dream cycle %s: %w", hash, err)
	}
	s.logger.Info("dream cycle reverted from feed card", "commit", hash)
	return nil
}

// handleDreamRevertPageAction restores one page of a cycle
// ("dream:revert-page:<hash>:<index>"). The index resolves against the cycle
// ledger's canonical page order — the same order dreamPageRevertActions minted
// the chips in.
func (s *Server) handleDreamRevertPageAction(item workfeed.Item, actionID string) error {
	rest := strings.TrimSpace(strings.TrimPrefix(actionID, dreamRevertPageActionPrefix))
	hash, idxText, ok := strings.Cut(rest, ":")
	idx, err := strconv.Atoi(idxText)
	if !ok || err != nil {
		return fmt.Errorf("unsupported dream page action %q", actionID)
	}
	if hash == "" {
		hash = strings.TrimSpace(item.RefID)
	}
	if hash == "" {
		return errors.New("dream card carries no cycle hash")
	}
	rec, found := s.wikiStore.DreamCycleChangesByCommit(hash)
	if !found {
		return fmt.Errorf("dream cycle %s has no change ledger", hash)
	}
	pages := wiki.DreamRevertPageList(rec)
	if idx < 0 || idx >= len(pages) {
		return fmt.Errorf("dream page revert index %d out of range (%d pages)", idx, len(pages))
	}
	page := pages[idx]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.wikiStore.RevertDreamPages(ctx, hash, []string{page}); err != nil {
		s.logger.Warn("dream page revert failed", "commit", hash, "page", page, "error", err)
		return fmt.Errorf("revert dream page %s: %w", page, err)
	}
	s.logger.Info("dream page reverted from feed card", "commit", hash, "page", page)
	return nil
}
