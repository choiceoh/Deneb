package server

// wiki-maint Trust Inbox card (위키 개선 방안 W15 — 모호 클래스만). The wiki
// accumulates advisory mismatches the autonomous lanes deliberately refuse to
// auto-repair — v1 surfaces the wiki↔mail person conflict scan (#4593's recall
// marker, now with a durable decision surface). Approve = 검토했음 (30일 조용),
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
	wikiMaintSource         = "wiki-maint"
	wikiMaintCheckInterval  = 12 * time.Hour
	wikiMaintDecisionQuiet  = 30 * 24 * time.Hour
	wikiMaintMaxConflictsOn = 5
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
	conflicts := s.wikiStore.PersonMailConflicts(ctx, wikiMaintMaxConflictsOn)
	if len(conflicts) == 0 {
		return nil
	}
	decisions := loadWikiMaintDecisions()
	now := time.Now().UnixMilli()
	quietMs := wikiMaintDecisionQuiet.Milliseconds()
	for _, c := range conflicts {
		if decided, ok := decisions[c.PagePath]; ok && now-decided < quietMs {
			continue
		}
		if err := s.postWikiMaintConflictCard(c); err != nil {
			s.logger.Warn("wiki-maint card post failed", "page", c.PagePath, "error", err)
		}
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
