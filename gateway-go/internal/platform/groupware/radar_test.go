package groupware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var radarMonday = time.Date(2026, 7, 13, 10, 0, 0, 0, radarKST)

func TestIsRadarBusinessHoursBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before open", time.Date(2026, 7, 13, 8, 29, 59, 0, radarKST), false},
		{"open inclusive", time.Date(2026, 7, 13, 8, 30, 0, 0, radarKST), true},
		{"before close", time.Date(2026, 7, 13, 18, 59, 59, 0, radarKST), true},
		{"close exclusive", time.Date(2026, 7, 13, 19, 0, 0, 0, radarKST), false},
		{"saturday", time.Date(2026, 7, 18, 12, 0, 0, 0, radarKST), false},
		{"UTC converted to KST", time.Date(2026, 7, 12, 23, 30, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRadarBusinessHours(tc.at); got != tc.want {
				t.Fatalf("IsRadarBusinessHours(%s) = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestRadarIntervalEnvironment(t *testing.T) {
	t.Setenv(radarIntervalMinutesEnv, "17")
	if got := NewRadar(RadarConfig{}).Interval(); got != 17*time.Minute {
		t.Fatalf("interval = %v, want 17m", got)
	}
	t.Setenv(radarIntervalMinutesEnv, "0")
	if got := NewRadar(RadarConfig{}).Interval(); got != DefaultRadarInterval {
		t.Fatalf("invalid interval = %v, want %v", got, DefaultRadarInterval)
	}
}

func TestRadarTriggerScanBypassesBusinessHours(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	sunday := time.Date(2026, 7, 12, 21, 0, 0, 0, radarKST)
	var calls []string
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return sunday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval("9", "nine")}, nil
			}
			return nil, nil
		},
		OnPending: func(_ context.Context, doc ApprovalSummary) error {
			calls = append(calls, doc.DocID)
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})

	// The periodic cycle stays gated off-hours…
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("off-hours periodic Run must not process: %v", calls)
	}
	// …but the phone-notification trigger scans immediately.
	if err := radar.TriggerScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"9"}) {
		t.Fatalf("TriggerScan calls = %v", calls)
	}
	// Dedup carries over: a duplicate push is a cheap no-op.
	if err := radar.TriggerScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("duplicate trigger re-notified: %v", calls)
	}
}

func TestRadarFirstRunCapUnchangedAndChange(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	pending := []ApprovalSummary{
		approval("4", "four"), approval("2", "two"), approval("1", "one"), approval("3", "three"),
	}
	var calls []string
	var listCalls []string
	radar := NewRadar(RadarConfig{
		StatePath:   statePath,
		MaxPerCycle: 2,
		Now:         func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, limit int) ([]ApprovalSummary, error) {
			if limit != radarApprovalListLimit {
				t.Fatalf("limit = %d, want %d", limit, radarApprovalListLimit)
			}
			listCalls = append(listCalls, folder)
			if folder == "pending" {
				return append([]ApprovalSummary(nil), pending...), nil
			}
			return nil, nil
		},
		OnPending: func(_ context.Context, doc ApprovalSummary) error {
			calls = append(calls, doc.DocID)
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})

	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"1", "2"}) {
		t.Fatalf("first capped calls = %v", calls)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Docs["3"].Notified || state.Docs["4"].Notified {
		t.Fatalf("overflow docs must remain retryable: %+v", state.Docs)
	}

	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"1", "2", "3", "4"}) {
		t.Fatalf("second cycle overflow calls = %v", calls)
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("unchanged snapshot invoked callback: %v", calls)
	}

	pending[1].Title = "two changed"
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"1", "2", "3", "4", "2"}) {
		t.Fatalf("changed doc calls = %v", calls)
	}
	if len(listCalls) != 8 || listCalls[len(listCalls)-2] != "pending" || listCalls[len(listCalls)-1] != "done" {
		t.Fatalf("list calls = %v", listCalls)
	}
}

func TestRadarPendingFailureStaysRetryable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	attempts := 0
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval("9", "retry")}, nil
			}
			return nil, nil
		},
		OnPending: func(context.Context, ApprovalSummary) error {
			attempts++
			if attempts == 1 {
				return errors.New("relay failed")
			}
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected callback failure")
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Docs["9"].Notified {
		t.Fatal("failed callback marked notified")
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

// A document whose ingest keeps failing must back off instead of burning a
// MaxPerCycle slot every cycle forever (live 2026-07: two unreadable docIds
// retried every 10 minutes for 8 days, 110 journal warnings, 2 of 3 slots).
func TestRadarPoisonDocBacksOffAndFreesSlot(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	var attempts []string
	// Docs drain in DocID order, so "1" is the poison doc that grabs the slot.
	const poison, healthy = "1", "2"
	radar := NewRadar(RadarConfig{
		StatePath:   statePath,
		Interval:    10 * time.Minute,
		MaxPerCycle: 1,
		Now:         func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval(poison, "unreadable"), approval(healthy, "fine")}, nil
			}
			return nil, nil
		},
		OnPending: func(_ context.Context, doc ApprovalSummary) error {
			attempts = append(attempts, doc.DocID)
			if doc.DocID == poison {
				return errors.New("exit status 1")
			}
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})

	// Cycle 1: poison takes the only slot and fails (attempt 1 = no backoff).
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected poison ingest failure")
	}
	// Cycle 2: poison retries immediately (transient-failure grace) and fails
	// again — that second failure arms a 10m backoff.
	now = now.Add(10 * time.Minute)
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected second poison failure")
	}
	// Cycle 3, inside the backoff window: the slot goes to the healthy doc.
	now = now.Add(5 * time.Minute)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatalf("healthy doc should drain while poison backs off: %v", err)
	}
	if want := []string{poison, poison, healthy}; !reflect.DeepEqual(attempts, want) {
		t.Fatalf("attempts = %v, want %v", attempts, want)
	}

	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Docs[poison].FailCount; got != 2 {
		t.Fatalf("poison failCount = %d, want 2", got)
	}
	if !state.Docs[healthy].Notified {
		t.Fatal("healthy doc never notified — poison still starving the lane")
	}

	// Backoff is bounded, so the doc self-heals once the upstream recovers
	// rather than being tombstoned.
	if got := radar.retryDelay(2); got != 10*time.Minute {
		t.Fatalf("retryDelay(2) = %s, want 10m", got)
	}
	if got := radar.retryDelay(4); got != 40*time.Minute {
		t.Fatalf("retryDelay(4) = %s, want 40m", got)
	}
	if got := radar.retryDelay(50); got != radarRetryBackoffCap {
		t.Fatalf("retryDelay(50) = %s, want cap %s", got, radarRetryBackoffCap)
	}

	// After the backoff elapses the doc is attempted again.
	now = now.Add(radarRetryBackoffCap)
	_ = radar.Run(context.Background())
	if attempts[len(attempts)-1] != poison {
		t.Fatalf("poison never retried after backoff; attempts = %v", attempts)
	}
}

// A re-drafted document (new fingerprint) must not inherit the old document's
// backoff.
func TestRadarFingerprintChangeClearsBackoff(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	title := "v1"
	attempts := 0
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Interval:  10 * time.Minute,
		Now:       func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval("5", title)}, nil
			}
			return nil, nil
		},
		OnPending: func(context.Context, ApprovalSummary) error {
			attempts++
			return errors.New("read failed")
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	// Fail 1 (no backoff), then fail 2 at +10m arms a 10m backoff.
	_ = radar.Run(context.Background())
	now = now.Add(10 * time.Minute)
	_ = radar.Run(context.Background())
	if attempts != 2 {
		t.Fatalf("attempts before backoff = %d, want 2", attempts)
	}
	// Still inside the backoff window: no new attempt.
	now = now.Add(5 * time.Minute)
	_ = radar.Run(context.Background())
	if attempts != 2 {
		t.Fatalf("attempts during backoff = %d, want 2", attempts)
	}
	// Re-draft: fingerprint changes, backoff clears, attempted again.
	title = "v2"
	_ = radar.Run(context.Background())
	if attempts != 3 {
		t.Fatalf("attempts after re-draft = %d, want 3", attempts)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Docs["5"].FailCount; got != 1 {
		t.Fatalf("failCount after re-draft = %d, want 1 (reset then re-failed)", got)
	}
}

// The cc lane shares the pending lane's slot-starvation defect.
func TestRadarCCPoisonDocBacksOffAndFreesSlot(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	var cc []ApprovalSummary
	var calls []string
	// cc docs drain in DocID order, so "1" is the poison doc.
	const poison, good = "1", "2"
	radar := NewRadar(RadarConfig{
		StatePath:     statePath,
		Interval:      10 * time.Minute,
		CCMaxPerCycle: 1,
		Now:           func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "cc" {
				return append([]ApprovalSummary(nil), cc...), nil
			}
			return nil, nil
		},
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
		OnCCNew: func(_ context.Context, doc ApprovalSummary) error {
			calls = append(calls, doc.DocID)
			if doc.DocID == poison {
				return errors.New("analysis failed")
			}
			return nil
		},
	})
	// Seed silently on an empty folder, then the docs arrive.
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cc = []ApprovalSummary{approval(poison, "bad"), approval(good, "fine")}
	// Fail 1 (no backoff), then fail 2 at +10m arms a 10m backoff.
	now = now.Add(10 * time.Minute)
	_ = radar.Run(context.Background())
	now = now.Add(10 * time.Minute)
	_ = radar.Run(context.Background())
	// Inside the backoff window: "good" finally gets the slot.
	now = now.Add(5 * time.Minute)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatalf("good cc doc should drain while poison backs off: %v", err)
	}
	if want := []string{poison, poison, good}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("cc calls = %v, want %v", calls, want)
	}
	state, _ := loadRadarState(statePath)
	if !state.CCDocs[good].Notified {
		t.Fatal("good cc doc never ingested — poison still starving the cc lane")
	}
	if got := state.CCDocs[poison].FailCount; got != 2 {
		t.Fatalf("cc poison failCount = %d, want 2", got)
	}
}

func TestRadarRequiresPositiveDoneBeforeResolution(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	pending := []ApprovalSummary{approval("7", "resolve safely")}
	var done []ApprovalSummary
	resolvedAttempts := 0
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return pending, nil
			}
			return done, nil
		},
		OnPending: func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(_ context.Context, doc ApprovalSummary) error {
			resolvedAttempts++
			if doc.DocID != "7" {
				t.Fatalf("resolved doc = %+v", doc)
			}
			if resolvedAttempts == 1 {
				return errors.New("ack failed")
			}
			return nil
		},
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending = nil
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resolvedAttempts != 0 {
		t.Fatal("pending disappearance alone resolved the card")
	}
	state, _ := loadRadarState(statePath)
	if _, ok := state.Docs["7"]; !ok {
		t.Fatal("disappeared pending doc was removed without done confirmation")
	}

	done = []ApprovalSummary{approval("7", "resolved")}
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected failed resolution callback")
	}
	state, _ = loadRadarState(statePath)
	if _, ok := state.Docs["7"]; !ok {
		t.Fatal("failed resolution callback removed state")
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ = loadRadarState(statePath)
	if _, ok := state.Docs["7"]; ok {
		t.Fatal("successful positive-done resolution retained state")
	}
}

func TestRadarCCSeedsSilentlyThenIngestsNewOnce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	cc := []ApprovalSummary{approval("100", "backlog A"), approval("101", "backlog B")}
	var ccCalls []string
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "cc" {
				return append([]ApprovalSummary(nil), cc...), nil
			}
			return nil, nil
		},
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
		OnCCNew: func(_ context.Context, doc ApprovalSummary) error {
			ccCalls = append(ccCalls, doc.DocID)
			return nil
		},
	})

	// First contact absorbs the historical backlog without callbacks.
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ccCalls) != 0 {
		t.Fatalf("seed run must not ingest: %v", ccCalls)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.CCSeededAt == 0 || !state.CCDocs["100"].Notified || !state.CCDocs["101"].Notified {
		t.Fatalf("seed state = %+v", state)
	}

	// A doc appearing after the seed ingests exactly once…
	cc = append(cc, approval("102", "new cc doc"))
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ccCalls, []string{"102"}) {
		t.Fatalf("cc calls = %v", ccCalls)
	}
	// …and fingerprint churn (other approvers acting) never re-triggers.
	cc[2].Status = "완결"
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ccCalls) != 1 {
		t.Fatalf("fingerprint churn re-ingested: %v", ccCalls)
	}
}

func TestRadarCCCapAndFailureStaysRetryable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	var cc []ApprovalSummary
	var calls []string
	failFirst := true
	radar := NewRadar(RadarConfig{
		StatePath:     statePath,
		CCMaxPerCycle: 1,
		Now:           func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "cc" {
				return append([]ApprovalSummary(nil), cc...), nil
			}
			return nil, nil
		},
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
		OnCCNew: func(_ context.Context, doc ApprovalSummary) error {
			calls = append(calls, doc.DocID)
			if failFirst {
				failFirst = false
				return errors.New("analysis failed")
			}
			return nil
		},
	})
	// Seed on an empty folder, then three docs arrive.
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	cc = []ApprovalSummary{approval("1", "a"), approval("2", "b"), approval("3", "c")}
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected cc ingest failure")
	}
	if !reflect.DeepEqual(calls, []string{"1"}) {
		t.Fatalf("cap-1 first cycle calls = %v", calls)
	}
	state, _ := loadRadarState(statePath)
	if state.CCDocs["1"].Notified {
		t.Fatal("failed ingest marked notified")
	}
	for range 3 {
		if err := radar.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"1", "1", "2", "3"}) {
		t.Fatalf("backlog drain calls = %v", calls)
	}
}

func TestRadarCCPrunesAfterRetentionAndFirstSeenIndexSkipsSeeded(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	cc := []ApprovalSummary{approval("5", "seeded old")}
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "cc" {
				return append([]ApprovalSummary(nil), cc...), nil
			}
			return nil, nil
		},
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
		OnCCNew:    func(context.Context, ApprovalSummary) error { return nil },
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Post-seed arrival is "new" for letters; the seeded doc is not.
	now = now.Add(time.Hour)
	cc = append(cc, approval("6", "fresh"))
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	idx := LoadRadarCCFirstSeenIndex(statePath)
	if _, ok := idx["5"]; ok {
		t.Fatalf("seeded doc leaked into first-seen index: %v", idx)
	}
	if _, ok := idx["6"]; !ok {
		t.Fatalf("fresh doc missing from first-seen index: %v", idx)
	}

	// Entries that fell off the list prune only after retention.
	cc = nil
	now = now.Add(time.Hour)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ := loadRadarState(statePath)
	if len(state.CCDocs) != 2 {
		t.Fatalf("pre-retention prune: %+v", state.CCDocs)
	}
	now = now.Add(radarCCRetention + time.Hour)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ = loadRadarState(statePath)
	if len(state.CCDocs) != 0 {
		t.Fatalf("post-retention entries remain: %+v", state.CCDocs)
	}
}

func TestRadarStateFileMode0600(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "nested", "radar.json")
	radar := NewRadar(RadarConfig{
		StatePath:  statePath,
		Now:        func() time.Time { return radarMonday },
		List:       func(context.Context, Config, string, int) ([]ApprovalSummary, error) { return nil, nil },
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
}

func approval(id, title string) ApprovalSummary {
	return ApprovalSummary{
		DocID: id, Title: title, DocNo: "EAP-" + id, Drafter: "drafter",
		Date: "2026-07-13", Status: "pending", Folder: "pending",
	}
}

func TestRadarEscalatesAtFourAndTwentyFourHoursOnce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	var levels []int
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval("1", "stale")}, nil
			}
			return nil, nil
		},
		OnPending: func(context.Context, ApprovalSummary) error { return nil },
		OnEscalated: func(_ context.Context, _ ApprovalSummary, level int, _ time.Duration) error {
			levels = append(levels, level)
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Hour)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Hour)
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(levels, []int{RadarEscalationLevelFourHours, RadarEscalationLevelTwentyFour}) {
		t.Fatalf("levels %v", levels)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Docs["1"].FirstSeenAt != radarMonday.UnixMilli() || state.Docs["1"].EscalationLevel != 2 {
		t.Fatalf("state %+v", state.Docs["1"])
	}
}

func TestRadarEscalationFailureRetriesAndStateMigrationPreservesAge(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	now := radarMonday
	if err := saveRadarState(statePath, radarState{Docs: map[string]radarDocState{"1": {Fingerprint: approvalFingerprint(approval("1", "stale")), Notified: true, LastSeenAt: now.Add(-5 * time.Hour).UnixMilli()}}}); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return now },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if folder == "pending" {
				return []ApprovalSummary{approval("1", "stale")}, nil
			}
			return nil, nil
		},
		OnPending: func(context.Context, ApprovalSummary) error { return nil },
		OnEscalated: func(context.Context, ApprovalSummary, int, time.Duration) error {
			attempts++
			if attempts == 1 {
				return errors.New("push failed")
			}
			return nil
		},
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected escalation failure")
	}
	state, _ := loadRadarState(statePath)
	if state.Docs["1"].EscalationLevel != 0 || state.Docs["1"].FirstSeenAt != now.Add(-5*time.Hour).UnixMilli() {
		t.Fatalf("failed state %+v", state.Docs["1"])
	}
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts %d", attempts)
	}
}

func TestRadarListFailureStreakAlertsOnce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	alerts := 0
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(context.Context, Config, string, int) ([]ApprovalSummary, error) {
			return nil, errors.New("exit status 1")
		},
		OnListFailed: func(_ context.Context, folder string, streak int, err error) error {
			alerts++
			if folder != "pending" {
				t.Fatalf("folder=%q", folder)
			}
			if streak != RadarListFailAlertAfter {
				t.Fatalf("streak=%d", streak)
			}
			if err == nil || !strings.Contains(err.Error(), "exit status 1") {
				t.Fatalf("err=%v", err)
			}
			return nil
		},
	})
	for i := 0; i < RadarListFailAlertAfter; i++ {
		if err := radar.Run(context.Background()); err == nil {
			t.Fatal("expected list failure")
		}
	}
	if alerts != 1 {
		t.Fatalf("alerts=%d want 1", alerts)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.ListFailAlerted || state.ListFailStreak != RadarListFailAlertAfter {
		t.Fatalf("state=%+v", state)
	}
	// Further failures stay quiet.
	if err := radar.Run(context.Background()); err == nil {
		t.Fatal("expected list failure")
	}
	if alerts != 1 {
		t.Fatalf("alerts after quiet=%d", alerts)
	}
}

func TestRadarListFailureClearsOnSuccess(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "radar.json")
	fail := true
	radar := NewRadar(RadarConfig{
		StatePath: statePath,
		Now:       func() time.Time { return radarMonday },
		List: func(_ context.Context, _ Config, folder string, _ int) ([]ApprovalSummary, error) {
			if fail {
				return nil, errors.New("boom")
			}
			return nil, nil
		},
		OnPending:  func(context.Context, ApprovalSummary) error { return nil },
		OnResolved: func(context.Context, ApprovalSummary) error { return nil },
	})
	_ = radar.Run(context.Background())
	fail = false
	if err := radar.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := loadRadarState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.ListFailStreak != 0 || state.ListFailAlerted || state.LastListError != "" {
		t.Fatalf("uncleared failure state: %+v", state)
	}
}
