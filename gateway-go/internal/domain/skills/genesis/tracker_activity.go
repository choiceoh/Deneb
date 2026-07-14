package genesis

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Evolution-activity liveness split out of tracker.go (pure move, no behavior
// change): activity recording, liveness snapshot persistence, and the
// evolve/rollback trigger wiring.

// RecordEvolutionActivity updates the Propus liveness heartbeat.
// kind is one of SkillActivityReview/Evolve/Genesis. ok=false also records the
// error so an operator can see WHY the loop stalled. Best-effort: a liveness
// write failure must never break the caller (this is observability, not state).
func (t *Tracker) RecordEvolutionActivity(kind string, ok bool, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordEvolutionActivityLocked(kind, ok, errMsg)
}

func (t *Tracker) recordEvolutionActivityLocked(kind string, ok bool, errMsg string) {
	state, err := t.loadLivenessLocked()
	if err != nil {
		// Start from a clean heartbeat rather than dropping the update.
		state = &SkillLivenessState{}
	}
	now := time.Now().UnixMilli()
	metricOnly := false
	switch kind {
	case skillActivityReview:
		state.LastReviewAt = now
		state.LastReviewOK = ok
	case SkillActivityReviewAttempt:
		state.ReviewAttempts++
		metricOnly = true
	case SkillActivityReviewSkipped:
		state.ReviewSkips++
		metricOnly = true
	case SkillActivityValidationRejected:
		state.ValidationRejections++
		metricOnly = true
	case skillActivityEvolve:
		state.LastEvolveAt = now
	case skillActivityGenesis:
		state.LastGenesisAt = now
	}
	if !metricOnly && !ok && errMsg != "" {
		// Truncate by rune, not byte: this surfaces in /health JSON, and a
		// byte slice can split a multi-byte UTF-8 sequence into replacement runes.
		state.LastError = genesiscommon.TruncateRunes(errMsg, 200)
		state.LastErrorAt = now
	} else if !metricOnly && ok {
		// A successful activity clears a stale error so /health doesn't keep
		// surfacing a failure that has since recovered (false-red).
		state.LastError = ""
		state.LastErrorAt = 0
	}
	if writeErr := t.saveLivenessLocked(state); writeErr != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: liveness write failed", "error", writeErr)
	}
}

// LivenessSnapshot returns the current Propus heartbeat for /health.
func (t *Tracker) LivenessSnapshot() SkillLivenessState {
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.loadLivenessLocked()
	if err != nil || state == nil {
		return SkillLivenessState{}
	}
	return *state
}

// SetEvolveTrigger wires the event-driven evolve. After `threshold` new skills
// are created (counted across restarts via the persisted sidecar), `fn` runs in
// the background; `minGap` suppresses a re-fire too soon after the previous
// evolve. threshold<=0 disables the trigger.
func (t *Tracker) SetEvolveTrigger(fn func(), threshold int, minGap time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evolveTrigger = fn
	t.evolveThreshold = threshold
	t.evolveMinGap = minGap
}

// SetRollback wires the post-evolve rollback. After a skill is evolved, its
// next uses are watched; `threshold` failures within the observation window
// (windowed, not strict-consecutive) fire `fn` to revert the evolution. fn
// reports whether the revert actually happened — a false return (missing
// backup, write error) is a failed rollback that leaves the regressing body
// live, and the tracker records it and clears the stashed baseline label so
// it can't mislabel a later resolution (RSI code eval H3). threshold<=0
// disables the watch.
func (t *Tracker) SetRollback(fn func(skillName string) bool, threshold int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollback = fn
	t.rollbackThreshold = threshold
}

// maybeFireEvolveLocked bumps the genesis counter and fires the evolve trigger
// in the background when it reaches the threshold and minGap has elapsed.
// Caller holds t.mu. The trigger (typically EvolutionTask.Run) updates
// LastEvolveAt itself, which feeds the next minGap check.
func (t *Tracker) maybeFireEvolveLocked() {
	if t.evolveTrigger == nil || t.evolveThreshold <= 0 {
		return
	}
	state, err := t.loadLivenessLocked()
	if err != nil {
		return
	}
	state.GenesisSinceEvolve++
	fire := false
	if state.GenesisSinceEvolve >= t.evolveThreshold && !t.triggerInflight {
		gapOK := t.evolveMinGap <= 0 || state.LastEvolveAt == 0 ||
			time.Since(time.UnixMilli(state.LastEvolveAt)) >= t.evolveMinGap
		if gapOK {
			state.GenesisSinceEvolve = 0
			t.triggerInflight = true
			fire = true
		}
	}
	if err := t.saveLivenessLocked(state); err != nil && t.logger != nil {
		t.logger.Warn("genesis-tracker: liveness counter write failed", "error", err)
	}
	if !fire {
		return
	}
	fn := t.evolveTrigger
	go func() {
		defer func() {
			if r := recover(); r != nil && t.logger != nil {
				t.logger.Error("genesis: evolve trigger panic", "panic", r)
			}
			t.mu.Lock()
			t.triggerInflight = false
			t.mu.Unlock()
		}()
		fn()
	}()
}

func (t *Tracker) loadLivenessLocked() (*SkillLivenessState, error) {
	state := &SkillLivenessState{}
	if t.livenessPath == "" {
		return state, nil
	}
	data, err := os.ReadFile(t.livenessPath)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: read liveness: %w", err)
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("genesis-tracker: parse liveness: %w", err)
	}
	return state, nil
}

func (t *Tracker) saveLivenessLocked(state *SkillLivenessState) error {
	if t.livenessPath == "" || state == nil {
		return nil
	}
	state.UpdatedAt = time.Now().UnixMilli()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("genesis-tracker: encode liveness: %w", err)
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(t.livenessPath, data, &atomicfile.Options{Perm: 0o600, Fsync: true})
}

// Close is a no-op (JSONL files are opened/closed per write).
func (t *Tracker) Close() error {
	return nil
}
