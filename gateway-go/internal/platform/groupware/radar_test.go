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
