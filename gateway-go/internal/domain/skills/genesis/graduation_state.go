package genesis

// Graduation state — the loop-owned unlock ledger (operator directive
// 2026-07-14: "잠금 해제도 에이전트에게 맡겨버려. 그래야 재귀적 자기개선이지").
//
// The graduation LADDER (rsi_ladder.go) scores evidence; this file is where
// an evidence-met row becomes an EXECUTED unlock. The flip consumers are the Go
// e-process, source-admission, and RSI-status paths plus the shell dispatch
// executor's daily cap. They consult the state in addition to their
// env/compiled defaults, with the operator's explicit env knob always winning.
//
// Trust architecture mirrors P2 auto-adoption exactly:
//   - evidence thresholds stay compiled (rsi_ladder.go) — the loop EXECUTES a
//     pre-declared policy, it can never rewrite it (this file and the ladder
//     are forbidden self-edit surfaces);
//   - kill switch DENEB_AUTO_GRADUATE=0 reverts to notify-only;
//   - the drift self-brake pauses auto-graduation with auto-adoption;
//   - every unlock/relock lands in the lifecycle ledger and surfaces as a
//     feed card with a 재잠금 veto — post-hoc operator control, not
//     pre-approval.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Graduation row keys — machine ids shared by the ladder, the state file, and
// the feed-card veto actions. Staged-source rows use "source:<prefix>".
const (
	graduationEProcess    = "eprocess-cutover"
	graduationDispatchCap = "dispatch-cap"
)

// graduationDispatchCapSteps is the pre-declared daily-dispatch-cap ladder
// (compiled policy — the loop EXECUTES it, it can never rewrite it). One step
// per graduation, and each rung must be earned by its OWN evidence cohort
// gathered AT the rung below: ladder_watch.go passes the current unlock's
// timestamp as the evidence floor, so the cohort that opened 2→4 can never also
// buy 4→8. Bounded ladder + per-rung evidence = no runaway ramp.
//
// 2026-07-27: this was a single const 4. Once the 2→4 unlock executed,
// ladderDispatchCapRow returned 완료 forever and the watch's
// !graduationUnlocked guard closed — no code path could ever grant the "further
// raise on fresh evidence at the new cap" the policy already promised, so the
// cap was stuck at the first rung by construction rather than by judgment.
var graduationDispatchCapSteps = []int{4, 8}

// nextGraduationDispatchCap returns the rung above current, or 0 at the top of
// the ladder (no further auto-raise available).
func nextGraduationDispatchCap(current int) int {
	for _, step := range graduationDispatchCapSteps {
		if step > current {
			return step
		}
	}
	return 0
}

// graduationDispatchCapRung reports the rung the executor is actually running
// and the next one to earn. A re-locked row drops the executor back to its
// compiled default, so the ladder re-offers the first rung rather than skipping
// ahead to a step the veto never let run.
func graduationDispatchCapRung(row graduationRow) (current, next int) {
	if !row.Unlocked {
		return 0, nextGraduationDispatchCap(0)
	}
	return row.Value, nextGraduationDispatchCap(row.Value)
}

// graduationRow is one executed unlock (or its later re-lock).
type graduationRow struct {
	Unlocked   bool   `json:"unlocked"`
	UnlockedAt int64  `json:"unlockedAt,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	// Value carries a row-specific unlock payload (dispatch-cap: the new cap).
	Value int `json:"value,omitempty"`
	// Auto distinguishes loop-executed unlocks from operator ones.
	Auto       bool   `json:"auto,omitempty"`
	RelockedAt int64  `json:"relockedAt,omitempty"`
	RelockNote string `json:"relockNote,omitempty"`
}

// graduationState is the whole unlock ledger, keyed by row.
type graduationState struct {
	Rows map[string]graduationRow `json:"rows"`
}

// graduationStatePath is FIXED under ~/.deneb/data (same convention as the
// Tracker ledgers, and the same gotcha: DENEB_STATE_DIR does not move it) so
// the Go paths and shell dispatch executor read the same unlock ledger.
func graduationStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", "graduation_state.json")
}

// loadGraduationState reads the unlock ledger; absent/corrupt reads as empty
// (locked everywhere) — the safe default.
func loadGraduationState() graduationState {
	st := graduationState{Rows: map[string]graduationRow{}}
	path := graduationStatePath()
	if path == "" {
		return st
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var loaded graduationState
	if json.Unmarshal(raw, &loaded) != nil || loaded.Rows == nil {
		return st
	}
	return loaded
}

// graduationUnlocked reports whether a row is currently unlocked.
func graduationUnlocked(key string) bool {
	return loadGraduationState().Rows[key].Unlocked
}

// saveGraduationState writes the ledger atomically (tmp+rename).
func saveGraduationState(st graduationState) error {
	path := graduationStatePath()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// unlockGraduation executes one ladder unlock: state write + lifecycle ledger
// (type "graduation", Propus-auditable). Idempotent for the same rung — a watch
// re-run never double-ledgers.
func (t *Tracker) unlockGraduation(key, evidence string, value int, auto bool) (bool, error) {
	st := loadGraduationState()
	row := st.Rows[key]
	// A strictly higher payload is a ladder STEP (dispatch-cap 4 → 8), not a
	// repeat of the same unlock. Rows that carry no payload are unaffected:
	// their value is 0 on both sides, so they stay strictly idempotent.
	if row.Unlocked && value <= row.Value {
		return false, nil
	}
	// A prior re-lock is a standing operator veto (재잠금 — post-hoc control). The
	// evidence gates are cumulative/monotonic, so without this the very next
	// evidence-met ladder-watch would re-unlock (and re-fire the "graduation
	// EXECUTED" card), silently reverting the veto. The AUTO path must honor it;
	// only an explicit operator (non-auto) unlock can override.
	if auto && row.RelockedAt > 0 {
		return false, nil
	}
	st.Rows[key] = graduationRow{
		Unlocked: true, UnlockedAt: time.Now().UnixMilli(),
		Evidence: evidence, Value: value, Auto: auto,
	}
	if err := saveGraduationState(st); err != nil {
		return false, err
	}
	return true, jsonlstore.Append(t.logPath, evolveLogEntry{
		Type: "graduation", SkillName: key, Reason: evidence, CreatedAt: time.Now().UnixMilli(),
	})
}

// RelockGraduation restores a lock (operator veto or a future regression
// watch). The row keeps its history; consumers read Unlocked=false.
func (t *Tracker) RelockGraduation(key, note string) error {
	st := loadGraduationState()
	row := st.Rows[key]
	if !row.Unlocked {
		return nil
	}
	row.Unlocked = false
	row.RelockedAt = time.Now().UnixMilli()
	row.RelockNote = note
	st.Rows[key] = row
	if err := saveGraduationState(st); err != nil {
		return err
	}
	return jsonlstore.Append(t.logPath, evolveLogEntry{
		Type: "graduation_relocked", SkillName: key, Reason: note, CreatedAt: time.Now().UnixMilli(),
	})
}

// graduatedDispatchSources lists the staged-source prefixes an executed
// graduation has admitted into the dispatch allowlist ("source:<prefix>"
// rows). Consulted by rsiSourceDispatchable alongside the compiled list.
func graduatedDispatchSources() []string {
	st := loadGraduationState()
	var out []string
	for key, row := range st.Rows {
		if !row.Unlocked {
			continue
		}
		if prefix, ok := cutGraduationSourceKey(key); ok {
			out = append(out, prefix)
		}
	}
	return out
}

// graduationSourceKey builds the state key for a staged-source row.
func graduationSourceKey(prefix string) string { return "source:" + prefix }

// cutGraduationSourceKey inverts graduationSourceKey.
func cutGraduationSourceKey(key string) (string, bool) {
	const p = "source:"
	if len(key) > len(p) && key[:len(p)] == p {
		return key[len(p):], true
	}
	return "", false
}
