package anomalywatch

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

const (
	// defaultInterval is how often the window is read. Hourly keeps each pass
	// small enough that a finding names a narrow time range — the reader's first
	// question about any anomaly is "when" — while costing one bounded local
	// call per hour.
	defaultInterval = time.Hour
	// defaultWindow overlaps the interval so an event landing near a pass
	// boundary is seen by one of the two passes rather than falling between them.
	defaultWindow = 90 * time.Minute
	// maxLedgerEntries is roughly a month of hourly passes.
	maxLedgerEntries = 800
)

// Task runs the watch on a schedule and writes each pass to the ledger.
//
// It never acts on what it finds. The ledger is read later by a person or by
// the operator's agent; that reader is the verification step, and inserting an
// automatic one here would mean acting on unverified claims.
type Task struct {
	// Lines fetches the window. Injected so the ring is resolved at RUN time
	// through the caller's log capture, never snapshotted at registration.
	Lines    func(sinceMs int64, limit int) []observe.LogLine
	Judge    Judge
	StateDir string
	Model    string
	Logger   *slog.Logger
}

// Name identifies the task in the autonomous scheduler.
func (t *Task) Name() string { return "anomaly-watch" }

// Interval honors DENEB_ANOMALY_WATCH_INTERVAL_MINUTES as a calibration knob.
func (t *Task) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_ANOMALY_WATCH_INTERVAL_MINUTES")); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			return time.Duration(mins) * time.Minute
		}
	}
	return defaultInterval
}

// Run reads one window, asks the local model about it, and records the pass.
func (t *Task) Run(ctx context.Context) error {
	if t == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := time.Now()
	var lines []observe.LogLine
	if t.Lines != nil {
		lines = t.Lines(now.Add(-defaultWindow).UnixMilli(), maxDigestLines)
	}
	digest := BuildDigest(lines)
	findings, gap := Inspect(ctx, t.Judge, digest)

	entry := Entry{
		At:          now.UTC().Format(time.RFC3339),
		WindowHours: int(defaultWindow / time.Hour),
		Examined:    digest.Examined,
		Findings:    findings,
		Gap:         gap,
		Model:       t.Model,
	}
	if err := Append(t.StateDir, entry, maxLedgerEntries); err != nil {
		// A pass that cannot be recorded is a pass that did not happen, as far
		// as the reader is concerned, so this is the one condition worth an
		// error return.
		logger.Error("anomaly-watch: 원장 기록 실패", "err", err)
		return err
	}

	// Every pass logs, findings or not — the same reason lanewatch does. A
	// watcher whose silence is indistinguishable from its absence is not
	// providing the assurance it appears to.
	logger.Info("anomaly-watch: 점검 완료",
		"examinedLines", digest.Examined.LogLines,
		"distinct", digest.Examined.DistinctMessages,
		"findings", len(findings),
		"gap", gap)
	for _, f := range findings {
		// Findings also reach the journal so the ledger is a convenience rather
		// than the only copy.
		logger.Warn("anomaly-watch: 이상 관측",
			"severity", f.Severity, "summary", f.Summary, "evidence", f.Evidence)
	}
	return nil
}
