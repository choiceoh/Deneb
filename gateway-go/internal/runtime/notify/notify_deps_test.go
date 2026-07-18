package notify

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

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

// Transitions fire exactly on state CHANGES: down on the first failing beat,
// nothing while it stays down, recovery on the first healthy beat.
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
	}})

	down, tr := s.probeDependencies()
	if len(down) != 0 || len(tr) != 0 {
		t.Fatalf("healthy beat: down=%v transitions=%v", down, tr)
	}

	healthy = false
	down, tr = s.probeDependencies()
	if len(down) != 1 || !strings.Contains(down[0], "bge-m3") {
		t.Fatalf("first failing beat down=%v", down)
	}
	if len(tr) != 1 || !tr[0].down || tr[0].name != "bge-m3" {
		t.Fatalf("first failing beat transitions=%v", tr)
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
