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
package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// selfCodingRetryInterval re-nudges an UNCHANGED pending set after this long —
// a turn that failed to clear the queue should not retry every 30 minutes, but
// the queue must not rot either. A CHANGED set (new candidate) nudges at the
// next tick regardless.
const selfCodingRetryInterval = 24 * time.Hour

// selfCodingNudgeState persists the last firing under the state dir
// (~/.deneb/heartbeat-selfcoding.json).
type selfCodingNudgeState struct {
	LastFingerprint string `json:"lastFingerprint"`
	LastNudgeAtMs   int64  `json:"lastNudgeAtMs"`
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
	if err := saveSelfCodingNudgeState(statePath, selfCodingNudgeState{
		LastFingerprint: fingerprint,
		LastNudgeAtMs:   now.UnixMilli(),
	}); err != nil {
		t.logger.Warn("heartbeat: self-coding nudge state save failed, skipping nudge", "error", err)
		return ""
	}
	t.logger.Info("heartbeat: self-coding review nudge fired", "proposed", count)
	return buildSelfCodingNudge(count)
}

// buildSelfCodingNudge renders the review contract the heartbeat turn receives.
// Scope discipline mirrors the candidates' own risk notes: state/data-file
// changes (SKILL.md, validation cases) may be executed directly; repository
// code is judged and recorded, never edited from a heartbeat turn.
func buildSelfCodingNudge(count int) string {
	return fmt.Sprintf(`[자가코딩 제안 검토] 자가개선 후보 %d건이 '제안됨' 상태로 대기 중입니다. 이번 점검에서 처리하세요 (최대 2건, 나머지는 다음 점검):
1) skill_lifecycle(action=status)로 pending self-corrections를 확인하고 각 후보의 evidence·targetFiles·risk를 읽으세요.
2) 스킬/테스트/문서 스코프(SKILL.md 수정, validation_case 추가 등 상태·데이터 파일)는 안전하면 직접 실행한 뒤 skill_lifecycle(action=self_correction_review, status=applied, reviewNote=수행 내용)로 기록하세요.
3) 코드 스코프(저장소 소스 수정)는 하트비트에서 직접 고치지 말고, 판정만 내려 self_correction_review(accepted 또는 rejected)에 근거를 남기세요.
4) 처리 결과 보고는 임원 판단이 필요한 발견일 때만 — 통상 처리는 리뷰 기록으로 충분합니다(NO_REPLY).`, count)
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
