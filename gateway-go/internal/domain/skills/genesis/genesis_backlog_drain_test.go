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
