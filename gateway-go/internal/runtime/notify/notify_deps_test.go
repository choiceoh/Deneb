package notify

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func depTestService(t *testing.T) *Service {
	t.Helper()
	s := NewService(session.NewManager(), slog.Default(), nil, nil)
	if s == nil {
		t.Fatal("service is nil")
	}
	return s
}

// Transitions fire exactly on confirmed state CHANGES: a first failing beat is
// shown in the heartbeat line only, the second consecutive failure alerts down,
// steady-down stays quiet, and recovery alerts once.
func TestProbeDependenciesTransitions(t *testing.T) {
	s := depTestService(t)
	healthy := true
	s.SetDependencyChecks([]DepCheck{{
		Name: "bge-m3",
		Check: func() error {
			if healthy {
				return nil
			}
			return errors.New("connection refused")
		},
	}}, "")

	down, tr := s.probeDependencies()
	if len(down) != 0 || len(tr) != 0 {
		t.Fatalf("healthy beat: down=%v transitions=%v", down, tr)
	}

	healthy = false
	down, tr = s.probeDependencies()
	if len(down) != 1 || !strings.Contains(down[0], "bge-m3") {
		t.Fatalf("first failing beat down=%v", down)
	}
	if len(tr) != 0 {
		t.Fatalf("first failing beat is only a pending blip, transitions=%v", tr)
	}

	down, tr = s.probeDependencies()
	if len(down) != 1 || !strings.Contains(down[0], "bge-m3") {
		t.Fatalf("second failing beat down=%v", down)
	}
	if len(tr) != 1 || !tr[0].down || tr[0].name != "bge-m3" {
		t.Fatalf("second failing beat transitions=%v", tr)
	}

	down, tr = s.probeDependencies()
	if len(down) != 1 || len(tr) != 0 {
		t.Fatalf("steady-down beat must not re-alert: down=%v transitions=%v", down, tr)
	}

	healthy = true
	_, tr = s.probeDependencies()
	if len(tr) != 1 || tr[0].down {
		t.Fatalf("recovery beat transitions=%v", tr)
	}
}

func TestProbeDependenciesSingleFailureClearsWithoutTransition(t *testing.T) {
	s := depTestService(t)
	healthy := false
	s.SetDependencyChecks([]DepCheck{{
		Name: "ocr",
		Check: func() error {
			if healthy {
				return nil
			}
			return errors.New("context deadline exceeded")
		},
	}}, "")

	down, tr := s.probeDependencies()
	if len(down) != 1 || len(tr) != 0 {
		t.Fatalf("single failing beat should be pending only: down=%v transitions=%v", down, tr)
	}

	healthy = true
	down, tr = s.probeDependencies()
	if len(down) != 0 || len(tr) != 0 {
		t.Fatalf("pending one-beat blip must clear silently: down=%v transitions=%v", down, tr)
	}
}

func TestComposeDepAlertAndLine(t *testing.T) {
	downAlert := composeDepAlert(depTransition{name: "bge-m3", down: true, err: errors.New("refused")})
	if !strings.Contains(downAlert, "🔌") || !strings.Contains(downAlert, "bge-m3") || !strings.Contains(downAlert, "refused") {
		t.Fatalf("down alert: %q", downAlert)
	}
	upAlert := composeDepAlert(depTransition{name: "bge-m3", down: false})
	if !strings.Contains(upAlert, "✅") || !strings.Contains(upAlert, "복구") {
		t.Fatalf("recovery alert: %q", upAlert)
	}
	if composeDepLine(nil) != "" {
		t.Fatal("healthy dep line must be empty")
	}
	if line := composeDepLine([]string{"a(x)", "b(y)"}); !strings.Contains(line, "a(x), b(y)") {
		t.Fatalf("dep line: %q", line)
	}
}

// No registered checks — probe is a no-op (fresh installs, tests).
func TestProbeDependenciesEmpty(t *testing.T) {
	s := depTestService(t)
	if down, tr := s.probeDependencies(); down != nil || tr != nil {
		t.Fatalf("empty checks: down=%v tr=%v", down, tr)
	}
}

// The down edge must survive a process restart: the first probe of a NEW
// service seeded from the state file sees a standing outage as known (no
// duplicate alert), while recovery still fires — with how long it was gone.
func TestDepDownPersistsAcrossRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "notify-dep-down.json")
	failing := func() error { return errors.New("connection refused") }
	healthy := func() error { return nil }

	// Process 1 observes and confirms the outage, then alerts once.
	s1 := depTestService(t)
	s1.SetDependencyChecks([]DepCheck{{Name: "asr", Check: failing}}, stateFile)
	if _, tr := s1.probeDependencies(); len(tr) != 0 {
		t.Fatalf("process 1 first failing beat should be pending only: %v", tr)
	}
	if _, tr := s1.probeDependencies(); len(tr) != 1 || !tr[0].down {
		t.Fatalf("process 1 second failing beat transitions=%v", tr)
	}

	// Process 2 (restart) sees the same standing outage — silence.
	s2 := depTestService(t)
	s2.SetDependencyChecks([]DepCheck{{Name: "asr", Check: failing}}, stateFile)
	down, tr := s2.probeDependencies()
	if len(down) != 1 {
		t.Fatalf("restart must still report the standing down-set: %v", down)
	}
	if len(tr) != 0 {
		t.Fatalf("restart must NOT re-fire the down alert: %v", tr)
	}

	// Recovery still alerts, and knows when the outage began.
	s3 := depTestService(t)
	s3.SetDependencyChecks([]DepCheck{{Name: "asr", Check: healthy}}, stateFile)
	if _, tr = s3.probeDependencies(); len(tr) != 1 || tr[0].down || tr[0].downSince.IsZero() {
		t.Fatalf("recovery after restart transitions=%v", tr)
	}

	// Recovery cleared the file: the next restart starts clean.
	s4 := depTestService(t)
	s4.SetDependencyChecks([]DepCheck{{Name: "asr", Check: failing}}, stateFile)
	if _, tr = s4.probeDependencies(); len(tr) != 0 {
		t.Fatalf("post-recovery first failure must be pending again: %v", tr)
	}
	if _, tr = s4.probeDependencies(); len(tr) != 1 || !tr[0].down {
		t.Fatalf("post-recovery confirmed outage must alert again: %v", tr)
	}
}

// Persisted names that left the check set are dropped, not resurrected.
func TestDepDownPruneRemovedChecks(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "notify-dep-down.json")
	if err := os.WriteFile(stateFile, []byte(`{"ghost":"2026-08-01T12:00:00+09:00"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := depTestService(t)
	s.SetDependencyChecks([]DepCheck{{Name: "asr", Check: func() error { return nil }}}, stateFile)
	if down, tr := s.probeDependencies(); len(down) != 0 || len(tr) != 0 {
		t.Fatalf("ghost dep must not surface: down=%v tr=%v", down, tr)
	}
}

// Recovery alert includes the outage duration when it is known and >=1m.
func TestComposeDepAlertRecoveryDuration(t *testing.T) {
	up := composeDepAlert(depTransition{name: "asr", down: false, downSince: time.Now().Add(-41 * time.Hour)})
	if !strings.Contains(up, "다운 1일 17시간") {
		t.Fatalf("recovery alert should carry duration: %q", up)
	}
	quick := composeDepAlert(depTransition{name: "asr", down: false, downSince: time.Now().Add(-10 * time.Second)})
	if strings.Contains(quick, "다운") {
		t.Fatalf("sub-minute blip should stay terse: %q", quick)
	}
}
