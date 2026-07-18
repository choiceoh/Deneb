// notify_deps.go — dependency (sidecar) health woven into the heartbeat.
//
// Motivation (live 2026-07-17/18): the BGE-M3 embedding sidecar died to a
// clean external SIGTERM and stayed down for 33 hours. The gateway KNEW —
// its embedding client logged "server unhealthy" every batch — but nothing
// surfaced it to the operator: the heartbeat only reported the gateway's own
// liveness. Silent degradation is the failure mode this file closes: every
// heartbeat now probes the registered dependencies, weaves failures into the
// beat line, and fires a distinct alert on each state TRANSITION (down and
// recovery), so an operator learns within one beat instead of a day later.
package notify

import (
	"fmt"
	"strings"
)

// DepCheck is one monitored dependency. Check returns nil when healthy; the
// error is shown (truncated) in the operator alert. Checks must be fast and
// non-blocking beyond a small internal timeout — they run on the heartbeat
// goroutine every beat.
type DepCheck struct {
	Name  string
	Check func() error
}

// SetDependencyChecks installs the dependency probes. Late-bind setter (the
// notify service is built in the Early phase; dependency handles like the
// embedding client exist only after the Session phase). Safe to call once
// before or after Start; replaces the whole set.
func (n *Service) SetDependencyChecks(checks []DepCheck) {
	n.depMu.Lock()
	defer n.depMu.Unlock()
	n.depChecks = checks
	if n.depDown == nil {
		n.depDown = make(map[string]bool, len(checks))
	}
}

// depTransition is one dependency state change observed by a beat.
type depTransition struct {
	name string
	down bool
	err  error
}

// probeDependencies runs every registered check, updates the known state, and
// returns the currently-down set (for the heartbeat line) plus the state
// transitions since the previous beat (for immediate alerts).
func (n *Service) probeDependencies() (down []string, transitions []depTransition) {
	n.depMu.Lock()
	checks := n.depChecks
	n.depMu.Unlock()
	if len(checks) == 0 {
		return nil, nil
	}

	for _, c := range checks {
		if c.Check == nil {
			continue
		}
		err := c.Check()
		isDown := err != nil

		n.depMu.Lock()
		wasDown := n.depDown[c.Name]
		n.depDown[c.Name] = isDown
		n.depMu.Unlock()

		if isDown {
			down = append(down, fmt.Sprintf("%s(%s)", c.Name, truncate(errString(err), 80)))
		}
		if isDown != wasDown {
			transitions = append(transitions, depTransition{name: c.Name, down: isDown, err: err})
		}
	}
	return down, transitions
}

// composeDepAlert formats the immediate operator alert for one transition.
func composeDepAlert(t depTransition) string {
	if t.down {
		return fmt.Sprintf("🔌 사이드카 다운: %s — %s", t.name, truncate(errString(t.err), 200))
	}
	return fmt.Sprintf("✅ 사이드카 복구: %s", t.name)
}

// composeDepLine formats the down-set suffix line for the heartbeat body.
// Empty when everything is healthy — the beat stays clean in the common case.
func composeDepLine(down []string) string {
	if len(down) == 0 {
		return ""
	}
	return "🔌 사이드카 다운: " + strings.Join(down, ", ")
}

func errString(err error) string {
	if err == nil {
		return "(unknown)"
	}
	return err.Error()
}
