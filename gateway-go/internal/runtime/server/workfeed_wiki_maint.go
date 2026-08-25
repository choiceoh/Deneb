package server

// wiki-maint Trust Inbox card (위키 개선 방안 W15 — 모호 클래스만). The wiki
// accumulates advisory mismatches the autonomous lanes deliberately refuse to
// auto-repair: the wiki↔mail person conflict scan (#4593) and the homonym scan
// (one 인물 page holding two employers' contacts). Approve = 검토했음 (30일 조용),
// reject = 그만 보기 (30일 조용). Neither writes to the wiki: repair stays with
// the operator (long-press 정정·피드백 → chat turn) or the dreamer — verify
// move/merge never becomes an approval card (W15 금지 조항).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

const (
	wikiMaintSource         = workfeed.SourceWikiMaint
	wikiMaintCheckInterval  = 12 * time.Hour
	wikiMaintDecisionQuiet  = 30 * 24 * time.Hour
	wikiMaintMaxConflictsOn = 5
	// Homonym candidates are resolved by hand (never auto-split), so a small
	// batch per cycle keeps the inbox readable — the live wiki holds ~15.
	wikiMaintMaxHomonyms = 3
)

// WikiMaintTask periodically scans advisory wiki mismatches and posts one
// decision card per finding. Registered on the autonomous scheduler like the
// other feed watches; its only state is the decisions file (30d quiet).
type WikiMaintTask struct {
	Server *Server
}

func (t *WikiMaintTask) Name() string            { return "wiki-maint" }
func (t *WikiMaintTask) Interval() time.Duration { return wikiMaintCheckInterval }
func (t *WikiMaintTask) Run(ctx context.Context) error {
	s := t.Server
	if s == nil || s.wikiStore == nil {
		return nil
	}
	// Retire first: a card whose finding no longer reproduces must leave the
	// inbox on its own. Without this, fixing the wiki did not clear the card —
	// the operator still had to press 확인 on a question that was already
	// answered, which is indistinguishable from the lane nagging about a
	// resolved issue (2026-08-25: three homonym cards outlived their fix by
	// twenty minutes and read as a re-ask).
	s.retireResolvedWikiMaintCards(ctx)

	conflicts := s.wikiStore.PersonMailConflicts(ctx, wikiMaintMaxConflictsOn)
	homonyms := s.wikiStore.HomonymPersonPages(wikiMaintMaxHomonyms)
	if len(conflicts) == 0 && len(homonyms) == 0 {
		return nil
	}
	decisions := loadWikiMaintDecisions()
	now := time.Now().UnixMilli()
	quietMs := wikiMaintDecisionQuiet.Milliseconds()
	quiet := func(key string) bool {
		decided, ok := decisions[key]
		return ok && now-decided < quietMs
	}
	for _, c := range conflicts {
		if quiet(c.PagePath) {
			continue
		}
		if err := s.postWikiMaintConflictCard(c); err != nil {
			s.logger.Warn("wiki-maint card post failed", "page", c.PagePath, "error", err)
		}
	}
	for _, h := range homonyms {
		// Keyed apart from the conflict card so deciding one does not silence
		// the other finding about the same person.
		if quiet(homonymDecisionKey(h.PagePath)) {
			continue
		}
		if err := s.postWikiMaintHomonymCard(h); err != nil {
			s.logger.Warn("wiki-maint homonym card post failed", "page", h.PagePath, "error", err)
		}
	}
	return nil
}

// homonymDecisionKey namespaces the homonym decision so it does not collide
// with the contact-conflict decision for the same page.
func homonymDecisionKey(pagePath string) string { return "homonym:" + pagePath }

// postWikiMaintHomonymCard surfaces one page that holds two identities. It
// proposes nothing: an automatic split is what caused the 2026-07-28 over-merge
// incident, so the card's job is to put the evidence in front of a human.
func (s *Server) postWikiMaintHomonymCard(h wiki.HomonymPerson) error {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return errors.New("native work feed unavailable")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "한 인물 페이지에 서로 다른 회사의 연락처가 함께 있습니다 — 동명이인이 한 노드로 합쳐졌을 수 있습니다.\n\n")
	fmt.Fprintf(&b, "- 인물: %s (`%s`)\n", h.Title, h.PagePath)
	fmt.Fprintf(&b, "- 회사 도메인: %s\n", strings.Join(h.Domains, ", "))
	b.WriteString("\n합쳐진 채로 두면 \"그 사람 연락처\"에 다른 회사 번호가 나갈 수 있습니다. ")
	b.WriteString("자동 분리는 하지 않습니다(과병합 사고 이후 규칙) — 길게 눌러 '정정·피드백'으로 어느 쪽이 누구인지 알려주시면 그때 나눕니다. ")
	b.WriteString("확인·그만 보기 모두 30일간 이 알림을 묵살합니다.")
	item := workfeed.Item{
		Source:   wikiMaintSource,
		Title:    "위키 정비: " + h.Title + " 동명이인 의심",
		Summary:  strings.Join(h.Domains, " · "),
		Status:   workfeed.StatusUnread,
		RefType:  "person-homonym",
		RefID:    homonymDecisionKey(h.PagePath),
		Question: true,
		Actions: []workfeed.Action{
			{ID: workfeedApproveAction, Kind: workfeed.ActionAck, Label: "확인"},
			{ID: workfeedRejectAction, Kind: workfeed.ActionAck, Label: "그만 보기"},
		},
		Body: b.String(),
	}
	if _, err := nf.Append(item); err != nil {
		return fmt.Errorf("post wiki-maint homonym card: %w", err)
	}
	return nil
}

// postWikiMaintConflictCard surfaces one wiki↔mail person mismatch. Delivery
// failure is returned (not swallowed) so the task log carries it; the decision
// file is only written on an explicit operator action.
func (s *Server) postWikiMaintConflictCard(c wiki.PersonMailConflict) error {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return errors.New("native work feed unavailable")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "위키 인물 페이지와 최근 메일의 연락처가 다릅니다 — 어느 쪽이 최신인지는 자동 판단하지 않습니다.\n\n")
	fmt.Fprintf(&b, "- 인물: %s (`%s`)\n", c.Title, c.PagePath)
	fmt.Fprintf(&b, "- 위키 연락처: %s\n", strings.Join(c.WikiEmails, ", "))
	fmt.Fprintf(&b, "- 최근 메일 발신: %s (`%s`)\n", c.MailFrom, c.MailPath)
	b.WriteString("\n확인은 30일간 이 알림을 조용히 묵살하고, 그만 보기도 마찬가지입니다. 위키를 바로잡으려면 이 카드를 길게 눌러 '정정·피드백'으로 알려주세요.")
	item := workfeed.Item{
		Source:  wikiMaintSource,
		Title:   "위키 정비: " + c.Title + " 연락처 불일치",
		Summary: fmt.Sprintf("위키 %s · 메일 %s", strings.Join(c.WikiEmails, ", "), c.MailFrom),
		Status:  workfeed.StatusUnread,
		RefType: "person-conflict",
		RefID:   c.PagePath,
		// Question renders the approve/reject decision chips inline.
		Question: true,
		Actions: []workfeed.Action{
			{ID: workfeedApproveAction, Kind: workfeed.ActionAck, Label: "확인"},
			{ID: workfeedRejectAction, Kind: workfeed.ActionAck, Label: "그만 보기"},
		},
		Body: b.String(),
	}
	if _, err := nf.Append(item); err != nil {
		return fmt.Errorf("post wiki-maint card: %w", err)
	}
	return nil
}

// handleWikiMaintCardAction records the operator's decision. Both approve and
// reject quiet the same finding for 30 days — the difference is only the audit
// trail; neither touches the wiki. An error keeps the card unsettled so the
// decision can be retried.
func (s *Server) handleWikiMaintCardAction(item workfeed.Item, actionID, _ string) error {
	refID := strings.TrimSpace(item.RefID)
	if refID == "" {
		return errors.New("wiki-maint card carries no page ref")
	}
	switch actionID {
	case workfeedApproveAction, workfeedRejectAction:
	default:
		return fmt.Errorf("unsupported wiki-maint action %q", actionID)
	}
	if err := recordWikiMaintDecision(refID); err != nil {
		return fmt.Errorf("record wiki-maint decision: %w", err)
	}
	s.logger.Info("wiki-maint finding decided from feed card", "page", refID, "action", actionID)
	return nil
}

// wikiMaintDecisions maps page ref → decided-at ms, next to the other
// workflow state files (DENEB_STATE_DIR/data).
func wikiMaintDecisionsPath() string {
	return filepath.Join(config.ResolveStateDir(), "data", "wiki_maint_decisions.json")
}

func loadWikiMaintDecisions() map[string]int64 {
	raw, err := os.ReadFile(wikiMaintDecisionsPath())
	if err != nil {
		return map[string]int64{}
	}
	var out map[string]int64
	if json.Unmarshal(raw, &out) != nil {
		return map[string]int64{}
	}
	return out
}

func recordWikiMaintDecision(refID string) error {
	decisions := loadWikiMaintDecisions()
	decisions[refID] = time.Now().UnixMilli()
	raw, err := json.Marshal(decisions)
	if err != nil {
		return err
	}
	path := wikiMaintDecisionsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// wikiMaintRetireScan bounds the active-card sweep. The feed is small and only
// wiki-maint rows are re-evaluated; the bound just keeps a runaway feed from
// turning a 12h task into a long scan.
const wikiMaintRetireScan = 200

// retireResolvedWikiMaintCards settles every active wiki-maint card whose
// finding no longer holds. Each card is re-checked BY ITS OWN REF (the
// single-page store APIs), never by diffing against the capped scan output: the
// scans stop early by design, so absence from their result is not evidence the
// finding is gone.
func (s *Server) retireResolvedWikiMaintCards(ctx context.Context) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	items, _, err := nf.List(wikiMaintRetireScan, false)
	if err != nil {
		s.logger.Warn("wiki-maint retire: 피드 조회 실패", "error", err)
		return
	}
	for _, item := range items {
		if item.Source != wikiMaintSource || item.Status == workfeed.StatusAcked {
			continue
		}
		if s.wikiMaintFindingStillOpen(ctx, item) {
			continue
		}
		if _, err := nf.Ack(item.ID); err != nil {
			s.logger.Warn("wiki-maint retire: 카드 정리 실패", "id", item.ID, "error", err)
			continue
		}
		s.logger.Info("wiki-maint: 해소된 카드 회수", "refType", item.RefType, "ref", item.RefID)
	}
}

// wikiMaintFindingStillOpen re-evaluates one card's finding. Unknown ref shapes
// stay open — retiring a card we cannot evaluate would silence it for good.
func (s *Server) wikiMaintFindingStillOpen(ctx context.Context, item workfeed.Item) bool {
	ref := strings.TrimSpace(item.RefID)
	if ref == "" {
		return true
	}
	switch item.RefType {
	case "person-homonym":
		_, open := s.wikiStore.HomonymPersonFor(strings.TrimPrefix(ref, "homonym:"))
		return open
	case "person-conflict":
		_, open := s.wikiStore.PersonMailConflictFor(ctx, ref)
		return open
	default:
		return true
	}
}
