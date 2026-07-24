// heartbeat_selfcoding.go — routes proposed self-improvement coding candidates
// into the heartbeat so they get CONSUMED instead of shelved.
//
// The self-coding capture (genesis tracker's self-correction candidates, shown
// in the native 자가코딩 개선 screen) proved its filter quality on first healthy
// operation (2026-07-04: two candidates from a real waste incident, one a
// genuinely good held-out-promotion proposal) — but both sat in "proposed" for
// a day until the operator noticed the badge and asked an external agent to
// act. That is the documented failure mode of this whole self-improvement
// family: capture without consumption (2026-07-03 skill-loop audit). This lane
// closes the gap: whenever proposed candidates exist, the next heartbeat turn
// is asked to review them via skill_lifecycle — execute the safe ones, record
// a reviewed verdict for the rest — so nothing waits on a human opening a
// screen. Deterministic count/fingerprint check per tick; LLM judgment stays
// inside the heartbeat turn.
package heartbeat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// selfCodingRetryInterval re-nudges an UNCHANGED pending set after this long.
// The fingerprint is derived purely from the proposed set (count + newest
// candidate id/updatedAt — see proposedSelfCoding in server_rpc_session.go), so
// an identical fingerprint means the turn consumed NOTHING: it neither applied a
// fix nor recorded a verdict for any candidate. Real progress (a candidate moved
// out of "proposed") changes the fingerprint and re-nudges at the very next
// tick, so this interval governs ONLY the did-nothing case — typically a turn
// that NO_REPLY'd past the review nudge without calling skill_lifecycle.
//
// It was 24h, which let a single ignored nudge lock the queue for a full day
// (observed 2026-07-09: nudge fired 19:36, turn returned NO_REPLY, queue stayed
// proposed and suppressed until the next day). Shortened to a few hours so an
// ignored queue is retried promptly while still not re-firing a cloud turn every
// tick. Pairs with the mandatory-verdict contract in buildSelfCodingNudge, which
// makes the turn actually act; a queue that STILL rots across retries trips the
// escalation below (selfCodingEscalateAfterIgnored — lever C).
const selfCodingRetryInterval = 2 * time.Hour

// selfCodingEscalateAfterIgnored arms the operator-escalation block once this
// many consecutive nudges consumed nothing (fingerprint unchanged from fire to
// fire). Two ignored nudges ≈ 4h of queue rot at the retry interval — the
// stall the mandatory-verdict contract alone cannot break when the model keeps
// shortcutting to NO_REPLY (the lever-C gap noted above).
const selfCodingEscalateAfterIgnored = 2

// selfCodingNudgeState persists the last firing under the state dir
// (~/.deneb/heartbeat-selfcoding.json).
type selfCodingNudgeState struct {
	LastFingerprint string `json:"lastFingerprint"`
	LastNudgeAtMs   int64  `json:"lastNudgeAtMs"`
	// IgnoredStreak counts consecutive nudges whose pending set came back
	// unchanged at the next fire — the turn consumed nothing. Reset the
	// moment the fingerprint moves; drives the escalation block in
	// buildSelfCodingNudge.
	IgnoredStreak int `json:"ignoredStreak,omitempty"`
}

func (t *heartbeatTask) selfCodingStatePath() string {
	return selfCodingStatePath(t.homeDir)
}

func selfCodingStatePath(homeDir string) string {
	return filepath.Join(homeDir, ".deneb", "heartbeat-selfcoding.json")
}

// lastSelfCodingNudgeAtMs reports when the self-coding review lane last fired,
// straight from the persisted marker (0 = never). Used by the miniapp funnel
// summary so the native screen can show consumption-lane liveness.
func lastSelfCodingNudgeAtMs() int64 {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return 0
	}
	return loadSelfCodingNudgeState(selfCodingStatePath(home)).LastNudgeAtMs
}

// LastSelfCodingNudgeAtMillis reports the most recent self-coding nudge time.
func LastSelfCodingNudgeAtMillis() int64 { return lastSelfCodingNudgeAtMs() }

// detectSelfCodingNudge returns the review-lane trigger text when proposed
// self-coding candidates are waiting, or "" when the queue is empty, the
// pending set is unchanged within the retry window, or no counter is wired.
// The marker persists on fire (fail-closed: a failed save skips the nudge so
// a broken state dir cannot re-fire a cloud turn every 30 minutes).
func (t *heartbeatTask) detectSelfCodingNudge(now time.Time) string {
	if t.proposedSelfCoding == nil || t.homeDir == "" {
		return ""
	}
	count, fingerprint := t.proposedSelfCoding()
	if count <= 0 {
		return ""
	}
	statePath := t.selfCodingStatePath()
	st := loadSelfCodingNudgeState(statePath)
	last := time.UnixMilli(st.LastNudgeAtMs)
	if st.LastFingerprint == fingerprint && st.LastNudgeAtMs > 0 &&
		now.After(last) && now.Sub(last) < selfCodingRetryInterval {
		return ""
	}
	// Re-firing on an unchanged fingerprint means the previous nudge consumed
	// nothing (see selfCodingRetryInterval); count those in a row so the
	// contract can escalate instead of retrying silently forever (lever C).
	ignored := 0
	if st.LastNudgeAtMs > 0 && st.LastFingerprint == fingerprint {
		ignored = st.IgnoredStreak + 1
	}
	if err := saveSelfCodingNudgeState(statePath, selfCodingNudgeState{
		LastFingerprint: fingerprint,
		LastNudgeAtMs:   now.UnixMilli(),
		IgnoredStreak:   ignored,
	}); err != nil {
		t.logger.Warn("heartbeat: self-coding nudge state save failed, skipping nudge", "error", err)
		return ""
	}
	if ignored >= selfCodingEscalateAfterIgnored {
		t.logger.Warn("heartbeat: self-coding nudges ignored repeatedly, escalating to operator report",
			"proposed", count, "ignoredStreak", ignored)
	}
	t.logger.Info("heartbeat: self-coding review nudge fired", "proposed", count, "ignoredStreak", ignored)
	return buildSelfCodingNudge(count, ignored)
}

// buildSelfCodingNudge renders the review contract the heartbeat turn receives.
// Scope discipline mirrors the candidates' own risk notes: state/data-file
// changes (SKILL.md, validation cases) may be executed directly; repository
// code is judged and recorded, never edited from a heartbeat turn.
//
// The contract is MANDATORY-VERDICT: the turn must record at least one
// skill_lifecycle(action=self_correction_review) before it ends. The prior
// wording offered NO_REPLY as the normal terminal ("통상 처리는 리뷰 기록으로
// 충분합니다(NO_REPLY)"), and the model took it as a shortcut — returning
// NO_REPLY with zero tool calls and leaving the queue untouched (2026-07-09).
// NO_REPLY now governs only the user-facing message (send only when executive
// judgment is warranted); it is not permission to skip the review itself.
//
// ignoredStreak ≥ selfCodingEscalateAfterIgnored appends the lever-C
// escalation: the turn must end with a short operator report (overriding the
// NO_REPLY default), so a queue the model keeps ignoring surfaces to a human
// instead of rotting behind silent retries.
func buildSelfCodingNudge(count, ignoredStreak int) string {
	nudge := fmt.Sprintf(`[자가코딩 제안 검토] 자가개선 후보 %d건이 '제안됨' 상태로 대기 중입니다. 이번 턴에서 반드시 처리하고 기록하세요 (최대 2건, 나머지는 다음 점검).

필수 절차 — 턴을 끝내기 전에 실제로 수행할 것:
1) skill_lifecycle(action=status)로 pending self-corrections를 열고 각 후보의 evidence·targetFiles·risk를 읽으세요.
   - 이번 status 출력에서 status=proposed인 후보만 리뷰 대상입니다. accepted/rejected/superseded/applied 후보나 과거 로그·스필오버·기억에서 본 오래된 id에는 self_correction_review를 다시 호출하지 마세요.
2) 스킬/테스트/문서 스코프(SKILL.md 수정, validation_case 추가 등 상태·데이터 파일)는 안전하면 직접 실행한 뒤 skill_lifecycle(action=self_correction_review, status=applied, reviewNote=수행 내용)로 기록하세요.
3) 코드 스코프(저장소 소스 수정)는 하트비트에서 직접 고치지 말고, 판정만 내려 skill_lifecycle(action=self_correction_review, status=accepted 또는 rejected, reviewNote=근거)로 기록하세요.
4) 안전하게 처리 못 할 후보도 방치 금지 — accepted(유효, 후속 필요) 또는 rejected(근거)로 판정해 '제안됨'에서 내보내세요.
5) 같은 후보 id에는 이번 턴에서 self_correction_review를 최대 1회만 호출하세요. invalid status transition 오류가 나오면 그 id는 이미 다른 경로에서 terminal 처리된 것으로 보고 즉시 중단하세요.

★필수: 최소 1건에 skill_lifecycle(action=self_correction_review) 호출을 남긴 뒤 턴을 종료하세요. 판정을 하나도 기록하지 않고 NO_REPLY로 끝내면 큐가 그대로 남아 재점검만 반복 소모합니다. 사용자 메시지는 임원 판단이 필요한 발견일 때만 작성하고, 그 외에는 리뷰 기록을 마친 뒤 NO_REPLY 하세요.`, count)
	if ignoredStreak >= selfCodingEscalateAfterIgnored {
		nudge += fmt.Sprintf(`

★에스컬레이션: 이 검토 넛지가 %d회 연속 아무 판정 없이 지나갔습니다(큐 변화 없음). 이번 턴은 침묵 종료 금지 — 위 절차를 수행한 뒤, 처리 결과(판정 요약) 또는 처리가 막힌 원인을 사용자에게 2~3문장으로 반드시 보고하세요. 이 보고는 NO_REPLY 규칙보다 우선합니다.`, ignoredStreak)
	}
	return nudge
}

func loadSelfCodingNudgeState(path string) selfCodingNudgeState {
	var st selfCodingNudgeState
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func saveSelfCodingNudgeState(path string, st selfCodingNudgeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
