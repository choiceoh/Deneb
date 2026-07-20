package genesis

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

type fakeBacklogGenesis struct {
	generated  []string
	skill      *generation.GeneratedSkill
	genErr     error
	persistErr error
	persisted  []*generation.GeneratedSkill
}

func (f *fakeBacklogGenesis) GenerateFromBacklog(_ context.Context, brief string) (*generation.GeneratedSkill, error) {
	f.generated = append(f.generated, brief)
	return f.skill, f.genErr
}

func (f *fakeBacklogGenesis) Persist(skill *generation.GeneratedSkill) error {
	if f.persistErr != nil {
		return f.persistErr
	}
	f.persisted = append(f.persisted, skill)
	return nil
}

func newDrainTask(t *testing.T, gen *fakeBacklogGenesis) (*GenesisBacklogDrainTask, *Tracker) {
	t.Helper()
	tracker := newTestTracker(t)
	return &GenesisBacklogDrainTask{
		Tracker:   tracker,
		Genesis:   gen,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		StatePath: filepath.Join(t.TempDir(), "drain_state.json"),
	}, tracker
}

func fileOpportunity(t *testing.T, tracker *Tracker, route, candidate, session string) {
	t.Helper()
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Candidate:  candidate,
		Route:      route,
		SessionKey: session,
		Source:     "test",
	}); err != nil {
		t.Fatal(err)
	}
}

// The strongest recurring genesis demand drains into a created skill; the
// pattern becomes terminal and is never re-attempted.
func TestGenesisBacklogDrain_RecurringDemandCreatesSkillOnce(t *testing.T) {
	gen := &fakeBacklogGenesis{skill: &generation.GeneratedSkill{
		Name: "multi-server-ops", Category: "ops", Description: "fleet-wide ops",
	}}
	task, tracker := newDrainTask(t, gen)
	// Recurring demand (3×) vs a one-off (floor is 2).
	fileOpportunity(t, tracker, "genesis", "multi-server-ops: 여러 서버에 동일한 시스템 변경을 적용", "s1")
	fileOpportunity(t, tracker, "genesis", "multi-server-ops: srv1~srv4에 동일 변경 적용 절차", "s2")
	fileOpportunity(t, tracker, "genesis", "multi-server-ops: 플릿 전체 시스템 변경", "s3")
	fileOpportunity(t, tracker, "genesis", "one-off-idea: 단발 아이디어", "s4")
	// Non-genesis routes are not drain material.
	fileOpportunity(t, tracker, "no-op", "multi-server-ops: no-op 에코", "s5")

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gen.generated) != 1 {
		t.Fatalf("want 1 generation attempt, got %d", len(gen.generated))
	}
	if len(gen.persisted) != 1 || gen.persisted[0].Name != "multi-server-ops" {
		t.Fatalf("persist mismatch: %+v", gen.persisted)
	}

	// The genesis event landed in the lifecycle log with the drain source.
	entries, err := tracker.RecentLifecycleLog(10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Type == "genesis" && e.SkillName == "multi-server-ops" && e.Source == "backlog-drain" {
			found = true
		}
	}
	if !found {
		t.Fatalf("genesis event with source=backlog-drain not logged: %+v", entries)
	}

	// Terminal outcome: a second run must not regenerate the same pattern
	// (nor fall through to the one-off, which is below the recurrence floor).
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gen.generated) != 1 {
		t.Fatalf("terminal pattern re-attempted: %d attempts", len(gen.generated))
	}
}

// A producer skip parks the pattern for the cooldown, then retries.
func TestGenesisBacklogDrain_SkipCooldownThenRetry(t *testing.T) {
	gen := &fakeBacklogGenesis{skill: nil} // producer says skip
	task, tracker := newDrainTask(t, gen)
	fileOpportunity(t, tracker, "genesis", "youtube-cards: 유튜브 요약 카드 절차", "s1")
	fileOpportunity(t, tracker, "genesis", "youtube-cards: URL 요약 카드", "s2")

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gen.generated) != 1 {
		t.Fatalf("skip must cool down, got %d attempts", len(gen.generated))
	}

	// Age the skip outcome past the cooldown → eligible again.
	state := loadGenesisDrainState(task.statePath())
	for k, o := range state {
		o.At = time.Now().Add(-genesisDrainRetryCooldown - time.Hour).UnixMilli()
		state[k] = o
	}
	if err := saveGenesisDrainState(task.statePath(), state); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gen.generated) != 2 {
		t.Fatalf("cooled-down skip must retry, got %d attempts", len(gen.generated))
	}
}

// Dedup from Persist is terminal — an existing skill covering the demand
// means the pattern never retries.
// TestGenesisBacklogDrain_EvolveBacklogReconciliation closes the evolve-route
// accounting gap (2026-07-20: 23 route=evolve opportunities with no consumption
// marking): consumed when the skill saw an evolve attempt after filing, stale
// past 14d with none, open (unmarked) while fresh. Accounting only — the drain
// never fires an evolve.
func TestGenesisBacklogDrain_EvolveBacklogReconciliation(t *testing.T) {
	gen := &fakeBacklogGenesis{}
	task, tracker := newDrainTask(t, gen)
	now := time.Now()

	// Consumed: opportunity filed, then the skill actually evolved.
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Route: "evolve", SkillName: "alpha-skill",
		Candidate: "improve alpha retries: flaky backoff handling",
		CreatedAt: now.Add(-2 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.LogEvolve("alpha-skill", "0.1.1", "tightened retries"); err != nil {
		t.Fatal(err)
	}
	// Stale: filed 20d ago, never attempted.
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Route: "evolve", SkillName: "beta-skill",
		Candidate: "rewrite beta guidance: verbose sections",
		CreatedAt: now.Add(-20 * 24 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	// Open: fresh and unconsumed — must stay unmarked.
	if err := tracker.RecordSkillOpportunity(SkillOpportunityRecord{
		Route: "evolve", SkillName: "gamma-skill",
		Candidate: "extend gamma coverage: new edge cases",
		CreatedAt: now.Add(-1 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	state := loadGenesisDrainState(task.StatePath)
	if got := state["evolve:improve alpha retries"]; got.Outcome != "evolve-consumed" || got.SkillName != "alpha-skill" {
		t.Fatalf("consumed disposition missing: %+v (state %+v)", got, state)
	}
	if got := state["evolve:rewrite beta guidance"]; got.Outcome != "evolve-stale" {
		t.Fatalf("stale disposition missing: %+v", got)
	}
	if _, ok := state["evolve:extend gamma coverage"]; ok {
		t.Fatal("fresh unconsumed pattern must stay open (unmarked)")
	}
	// Idempotent: a second run adds no new dispositions.
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again := loadGenesisDrainState(task.StatePath); len(again) != 2 {
		t.Fatalf("reconcile must be idempotent, state = %+v", again)
	}
}

// TestEvolveAttemptedSinceMatchesSkillAndWindow pins the reconciler's
// consumption evidence: committed evolves and executed evolve-route proposals
// count, other skills and out-of-window records do not.
func TestEvolveAttemptedSinceMatchesSkillAndWindow(t *testing.T) {
	tracker := newTestTracker(t)
	if err := tracker.LogEvolve("alpha", "0.1.1", "d"); err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	if ok, err := tracker.EvolveAttemptedSince("alpha", nowMs-time.Hour.Milliseconds()); err != nil || !ok {
		t.Fatalf("committed evolve inside window not seen (ok=%v err=%v)", ok, err)
	}
	if ok, _ := tracker.EvolveAttemptedSince("alpha", nowMs+time.Hour.Milliseconds()); ok {
		t.Fatal("future window must not match")
	}
	if ok, _ := tracker.EvolveAttemptedSince("beta", 0); ok {
		t.Fatal("other skill must not match")
	}
	if err := tracker.LogEvolutionProposal(EvolutionProposalRecord{
		Route: "evolve", SkillName: "gamma", Executed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tracker.EvolveAttemptedSince("gamma", nowMs-1000); !ok {
		t.Fatal("executed evolve proposal must count as an attempt")
	}
}

func TestGenesisBacklogDrain_DedupIsTerminal(t *testing.T) {
	gen := &fakeBacklogGenesis{
		skill:      &generation.GeneratedSkill{Name: "already-there", Category: "ops", Description: "d"},
		persistErr: generation.ErrSkillDeduped,
	}
	task, tracker := newDrainTask(t, gen)
	fileOpportunity(t, tracker, "genesis", "already-there: 이미 있는 스킬 수요", "s1")
	fileOpportunity(t, tracker, "genesis", "already-there: 중복 수요", "s2")

	for i := 0; i < 2; i++ {
		if err := task.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(gen.generated) != 1 {
		t.Fatalf("deduped pattern re-attempted: %d", len(gen.generated))
	}
}

// Below the recurrence floor nothing drains — one session's opinion is not
// demand.
func TestGenesisBacklogDrain_FloorHolds(t *testing.T) {
	gen := &fakeBacklogGenesis{}
	task, tracker := newDrainTask(t, gen)
	fileOpportunity(t, tracker, "genesis", "solo-signal: 한 번 나온 아이디어", "s1")
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gen.generated) != 0 {
		t.Fatalf("single signal drained: %d", len(gen.generated))
	}
}

// Generation errors back off like skips (non-terminal).
func TestGenesisBacklogDrain_ErrorBacksOff(t *testing.T) {
	gen := &fakeBacklogGenesis{genErr: errors.New("llm down")}
	task, tracker := newDrainTask(t, gen)
	fileOpportunity(t, tracker, "genesis", "flaky-lane-signals: 수요 신호", "s1")
	fileOpportunity(t, tracker, "genesis", "flaky-lane-signals: 수요 신호 재발", "s2")
	for i := 0; i < 2; i++ {
		if err := task.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(gen.generated) != 1 {
		t.Fatalf("error outcome must cool down, got %d attempts", len(gen.generated))
	}
}
