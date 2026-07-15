package servermail

// Graduation-ladder READY card — the LadderWatchTask's operator surface. A
// ladder row whose evidence crossed its threshold is a standing decision the
// GRAD dashboard alone would leave unnoticed; the card brings it to the feed
// once per transition. Informational + ack (the actual flips are env knobs /
// allowlist edits done in a session, not chat actions).

import (
	"errors"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const ladderReadySource = "genesis-ladder"

// PostLadderReadyCard surfaces one graduation-ladder row that just reached
// READY. Delivery failure is returned so LadderWatch does not consume the
// transition before the operator-facing card is durable. Implements
// serverport.Host's PostLadderReadyCard surface.
func (m *Manager) PostLadderReadyCard(title, detail string) error {
	nf := m.NativeWorkFeedStore()
	if nf == nil {
		return errors.New("native work feed unavailable")
	}
	item := workfeed.Item{
		Source:  ladderReadySource,
		Title:   "졸업 사다리 준비됨: " + title,
		Summary: detail,
		Status:  "unread",
		Body: fmt.Sprintf(`자율성 졸업 사다리의 한 행이 증거 임계에 도달했습니다.

- 행: %s
- 증거: %s

잠금 해제는 자동으로 실행되지 않습니다 — 결정하시려면 채팅에서 지시하세요 (해당 노브/허용목록 변경은 세션 작업입니다). 상세 근거는 더보기 → 재귀적 자가개선의 '졸업 사다리' 카드에 있습니다.`, title, detail),
		Actions: []workfeed.Action{
			{ID: "ladder:ack", Kind: workfeed.ActionAck, Label: "확인"},
		},
	}
	if _, err := nf.Append(item); err != nil {
		m.Host.Logger().Warn("ladder-ready card post failed", "row", title, "error", err)
		return fmt.Errorf("post ladder-ready card: %w", err)
	}
	return nil
}

// ladderActionRelockPrefix carries the graduation row key in the action id:
// "ladder:relock:<row-key>".
const ladderActionRelockPrefix = "ladder:relock:"

// PostGraduationCard surfaces one EXECUTED unlock (operator directive
// 2026-07-14: the loop flips evidence-met locks itself) — notification with a
// post-hoc 재잠금 veto, mirroring the P2 auto-adoption card. Implements
// serverport.Host's PostGraduationCard surface.
func (m *Manager) PostGraduationCard(key, title, evidence string) {
	nf := m.NativeWorkFeedStore()
	if nf == nil {
		return
	}
	item := workfeed.Item{
		Source:  ladderReadySource,
		Title:   "졸업 실행: " + title,
		Summary: evidence,
		Status:  "unread",
		Body: fmt.Sprintf(`자율성 졸업 사다리의 한 행이 증거 임계를 충족해 자동으로 잠금 해제되었습니다.

- 행: %s
- 증거: %s

임계값 정책은 코드에 고정되어 있고 루프는 이를 실행만 합니다 (수용회로 forbidden). 되돌리려면 아래 재잠금을 누르세요 — 즉시 잠금 상태로 복원되고 원장에 기록됩니다. 킬 스위치: DENEB_AUTO_GRADUATE=0.`, title, evidence),
		Actions: []workfeed.Action{
			{ID: ladderActionRelockPrefix + key, Kind: workfeed.ActionAck, Label: "재잠금"},
		},
	}
	if _, err := nf.Append(item); err != nil {
		m.Host.Logger().Warn("graduation card post failed", "row", key, "error", err)
	}
}

// handleLadderCardAction durably applies the operator's 재잠금 veto. An error
// keeps the card unsettled so the operator can retry instead of losing the veto.
func (m *Manager) handleLadderCardAction(_ workfeed.Item, actionID string) error {
	tracker := m.Host.GenesisTracker()
	if tracker == nil {
		return errors.New("genesis tracker unavailable")
	}
	if !strings.HasPrefix(actionID, ladderActionRelockPrefix) {
		return fmt.Errorf("unsupported ladder action %q", actionID)
	}
	key := strings.TrimPrefix(actionID, ladderActionRelockPrefix)
	if key == "" {
		return errors.New("graduation row key is empty")
	}
	if err := tracker.RelockGraduation(key, "operator relocked from feed card"); err != nil {
		m.Host.Logger().Warn("graduation relock failed", "row", key, "error", err)
		return fmt.Errorf("relock graduation %s: %w", key, err)
	}
	m.Host.Logger().Info("graduation relocked from feed card", "row", key)
	return nil
}
