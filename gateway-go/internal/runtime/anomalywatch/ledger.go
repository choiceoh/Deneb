// Package anomalywatch has the local model read the gateway's own recent
// runtime and write down what looks wrong, so a later reader — the operator's
// agent, checking the runtime hours or days after the fact — finds a timestamped
// account instead of an empty journal.
//
// # Why a ledger and not a dashboard
//
// Deneb already has a self-status digest (internal/ai/observatory), and it is
// good at what it does. But it is a SNAPSHOT: it answers "what is true now".
// It structurally cannot answer "the mail lane went quiet at 03:00 for four
// hours and then recovered", because at 03:00 nobody was reading it. Every
// existing watcher shares that shape — the error miner keys on signatures it
// was told to look for, lanewatch keys on silence budgets someone wrote down.
// All of them detect what was anticipated.
//
// This lane is the one that does not need to have anticipated. It hands a
// bounded window of the gateway's own logs to the local model and asks the open
// question: does anything here look wrong? The answer is written down and
// nothing else happens.
//
// # Why observation-only, and who verifies
//
// A model reading logs produces plausible-sounding claims with no ground truth,
// and would be a poor decision-maker: over one investigation day, eight separate
// log-derived conclusions ("the embedder died", "recall regressed", "skill
// generation stopped") were each wrong and each cost real work to disprove. That
// is an argument against ACTING on these findings, not against recording them.
// The reader is the verifier: a finding that quotes its evidence verbatim is
// dismissed in seconds, while an anomaly nobody was awake for is otherwise never
// seen at all. So every finding carries the log line it came from, and the lane
// never edits, restarts, or escalates anything.
package anomalywatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one watch pass, appended as a single JSONL line.
//
// A pass is recorded even when it finds nothing. That is not symmetry for its
// own sake: without it, "the window was clean" and "the watcher never ran" are
// the same empty ledger, which is precisely the confusion this whole family of
// lanes exists to remove.
type Entry struct {
	At string `json:"at"`
	// WindowMinutes is the window the pass ASKED for. What it actually got is
	// Examined.CoveredMinutes, and the two diverge routinely.
	WindowMinutes int `json:"windowMinutes"`
	// Examined is what the pass actually read. A finding count means nothing
	// without it — zero findings over zero lines is not a healthy runtime.
	Examined Examined  `json:"examined"`
	Findings []Finding `json:"findings,omitempty"`
	// Gap records why a pass could not reach a verdict (model unreachable,
	// unparseable reply). A gap is louder than a clean pass, so it is never
	// folded into "no findings".
	Gap string `json:"gap,omitempty"`
	// Model names who judged, so a change in finding quality can be traced to a
	// change in the judge rather than to the runtime.
	Model string `json:"model,omitempty"`
}

// Examined describes the window the pass could actually see.
type Examined struct {
	LogLines int `json:"logLines"`
	Warns    int `json:"warns"`
	Errors   int `json:"errors"`
	// CoveredMinutes is how much of the requested window the log ring could
	// actually supply. The ring lives in-process, so every gateway restart
	// empties it — and with auto-deploy restarting the gateway several times a
	// day, a truncated window is the NORMAL case, not an edge one.
	//
	// Without this field the two readings that matter most are identical: a
	// quiet hour and a six-minute-old process both write "logLines: 1". The
	// first says the runtime is healthy; the second says almost nothing was
	// observed. Recording only the requested window would make the ledger
	// assert the first whenever the second was true.
	CoveredMinutes int `json:"coveredMinutes"`
	// Partial marks a window the process was not alive long enough to fill.
	Partial bool `json:"partial,omitempty"`
	// DistinctMessages is the number of unique log messages, which separates a
	// window of 200 lines of one repeating error from 200 distinct events.
	DistinctMessages int `json:"distinctMessages"`
}

// Finding is one thing the model thought was wrong.
type Finding struct {
	// Severity is the model's own read, kept advisory. It orders a reader's
	// attention; it does not gate anything.
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	// Evidence is the log line, verbatim. This is the load-bearing field: it is
	// what lets a reader confirm or dismiss without re-deriving the claim, and
	// a finding that cannot point at a line is dropped before it is written.
	Evidence string `json:"evidence"`
	// WhyItMatters is the model's account of the consequence. Frequently the
	// part that is wrong, which is why it is stored beside the evidence rather
	// than in place of it.
	WhyItMatters string `json:"whyItMatters,omitempty"`
}

// LedgerPath is where passes accumulate.
func LedgerPath(stateDir string) string {
	return filepath.Join(stateDir, "anomaly-watch.jsonl")
}

// Append writes one pass to the ledger, trimming the file when it grows past
// maxEntries so an always-on lane cannot fill the disk.
func Append(stateDir string, e Entry, maxEntries int) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("anomalywatch: empty state dir")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("anomalywatch: state dir: %w", err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("anomalywatch: marshal: %w", err)
	}
	path := LedgerPath(stateDir)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("anomalywatch: open ledger: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("anomalywatch: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("anomalywatch: close: %w", err)
	}
	if maxEntries > 0 {
		trim(path, maxEntries)
	}
	return nil
}

// trim keeps the newest maxEntries lines. Best-effort: a ledger that fails to
// shrink is not worth failing a pass over, since the pass itself succeeded.
func trim(path string, maxEntries int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= maxEntries {
		return
	}
	kept := strings.Join(lines[len(lines)-maxEntries:], "\n") + "\n"
	_ = os.WriteFile(path, []byte(kept), 0o644)
}

// Read returns passes newest-first, at most limit of them. This is the read
// path a later reader uses; it is deliberately plain, because a ledger that
// needs a query language to inspect will not be inspected.
func Read(stateDir string, limit int) ([]Entry, error) {
	data, err := os.ReadFile(LedgerPath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e Entry
		// A corrupt line is skipped rather than failing the read: a partially
		// written tail must not hide every pass before it.
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At > entries[j].At })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// Since returns passes newer than cutoff, newest-first — the shape a reader
// actually wants ("what happened since I last looked").
func Since(stateDir string, cutoff time.Time) ([]Entry, error) {
	all, err := Read(stateDir, 0)
	if err != nil {
		return nil, err
	}
	stamp := cutoff.UTC().Format(time.RFC3339)
	var out []Entry
	for _, e := range all {
		if e.At >= stamp {
			out = append(out, e)
		}
	}
	return out, nil
}

// openAppend is the ledger's own open path, exported to the package so tests
// can simulate a truncated write without duplicating the flags.
func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
