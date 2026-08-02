// notify_deps.go — dependency (sidecar) health woven into the heartbeat.
//
// Motivation (live 2026-07-17/18): the embedding sidecar (then BGE-M3) died to a
// clean external SIGTERM and stayed down for 33 hours. The gateway KNEW —
// its embedding client logged "server unhealthy" every batch — but nothing
// surfaced it to the operator: the heartbeat only reported the gateway's own
// liveness. Silent degradation is the failure mode this file closes: every
// heartbeat now probes the registered dependencies, weaves failures into the
// beat line, and fires a distinct alert on each state TRANSITION (down and
// recovery), so an operator learns within one beat instead of a day later.
//
// Down-state is persisted (2026-08-03): the transition edge used to live only
// in memory, so every gateway restart re-fired the "down" push for a fault the
// operator already knew about — a 41h ASR outage produced 15 identical alerts,
// one per deploy. Now the down-set survives restarts in a small state file;
// a standing outage alerts once when first observed and once on recovery,
// regardless of how many times the gateway itself bounces in between.
package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
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
//
// stateFile, when non-empty, is where the standing down-set is persisted
// across restarts. Names loaded from it that are still in the check set are
// seeded as already-down, so the first probe after a restart treats a
// standing outage as known instead of re-alerting; names no longer checked
// are dropped. Empty stateFile (tests) keeps everything in memory.
func (n *Service) SetDependencyChecks(checks []DepCheck, stateFile string) {
	persisted := loadDepDown(stateFile)

	n.depMu.Lock()
	defer n.depMu.Unlock()
	n.depChecks = checks
	n.depStateFile = stateFile
	if n.depDown == nil {
		n.depDown = make(map[string]time.Time, len(checks))
	}
	installed := make(map[string]bool, len(checks))
	for _, c := range checks {
		installed[c.Name] = true
	}
	// Prune deps that left the check set (config change) so their stale
	// down-state can neither suppress a future alert nor linger on disk.
	for name := range n.depDown {
		if !installed[name] {
			delete(n.depDown, name)
		}
	}
	// Seed persisted outages — but never clobber live in-process knowledge
	// (a replace-call after probes have run knows more than the disk does).
	for name, since := range persisted {
		if installed[name] && n.depDown[name].IsZero() {
			n.depDown[name] = since
		}
	}
}

// depTransition is one dependency state change observed by a beat.
type depTransition struct {
	name string
	down bool
	err  error
	// downSince is when the outage began (recovery transitions only) —
	// carried so the recovery alert can say how long the dep was gone,
	// even when the down edge was observed by a previous process.
	downSince time.Time
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
		since := n.depDown[c.Name]
		wasDown := !since.IsZero()
		switch {
		case isDown && !wasDown:
			since = time.Now()
			n.depDown[c.Name] = since
		case !isDown && wasDown:
			delete(n.depDown, c.Name)
		}
		n.depMu.Unlock()

		if isDown {
			down = append(down, fmt.Sprintf("%s(%s)", c.Name, truncate(errString(err), 80)))
		}
		if isDown != wasDown {
			transitions = append(transitions, depTransition{name: c.Name, down: isDown, err: err, downSince: since})
		}
	}
	if len(transitions) > 0 {
		n.persistDepDown()
	}
	return down, transitions
}

// persistDepDown writes the standing down-set to the state file. Called only
// on transitions (rare), never on quiet beats. Snapshot under the lock, file
// I/O outside it. Failure is a recoverable anomaly: worst case the next
// restart re-alerts once — exactly the pre-persistence behavior.
func (n *Service) persistDepDown() {
	n.depMu.Lock()
	path := n.depStateFile
	snap := make(map[string]time.Time, len(n.depDown))
	for name, since := range n.depDown {
		snap[name] = since
	}
	n.depMu.Unlock()
	if path == "" {
		return
	}

	data, err := json.Marshal(snap)
	if err == nil {
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o600); err == nil {
			err = os.Rename(tmp, path)
		}
	}
	if err != nil {
		n.logger.Warn("dep down-state persist failed", "path", path, "error", err)
	}
}

// loadDepDown reads the persisted down-set. Missing file (fresh install,
// healthy fleet) and parse errors both yield nil — persistence is an
// alert-dedup optimization, never a probe correctness input.
func loadDepDown(path string) map[string]time.Time {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]time.Time
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// composeDepAlert formats the immediate operator alert for one transition.
func composeDepAlert(t depTransition) string {
	if t.down {
		return fmt.Sprintf("🔌 사이드카 다운: %s — %s", t.name, truncate(errString(t.err), 200))
	}
	if !t.downSince.IsZero() {
		if d := time.Since(t.downSince); d >= time.Minute {
			return fmt.Sprintf("✅ 사이드카 복구: %s (다운 %s)", t.name, humanDownFor(d))
		}
	}
	return fmt.Sprintf("✅ 사이드카 복구: %s", t.name)
}

// humanDownFor renders an outage duration for the recovery alert, Korean-first
// and at most two units — "7분", "3시간", "1일 17시간".
func humanDownFor(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d분", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d시간", int(d.Hours()))
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%d일", days)
	}
	return fmt.Sprintf("%d일 %d시간", days, hours)
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
