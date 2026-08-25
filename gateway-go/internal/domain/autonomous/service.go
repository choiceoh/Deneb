package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Notifier delivers significant events to the user.
// Implemented by the server layer to send messages to the user's channel.
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// EventListener receives lifecycle events.
type EventListener func(event CycleEvent)

// CycleEvent describes a lifecycle event for external consumers.
type CycleEvent struct {
	Type        string       `json:"type"` // "dreaming_started", "dreaming_completed", "dreaming_failed"
	DreamReport *DreamReport `json:"dreamReport,omitempty"`
	Ts          int64        `json:"ts"` //nolint:staticcheck // ST1003 — JSON field name
}

// Service manages the AuroraDream memory consolidation lifecycle
// and registered periodic tasks (e.g., Gmail polling).
type Service struct {
	mu     sync.Mutex
	logger *slog.Logger

	// Service-level context for propagation to async operations.
	svcCtx    context.Context
	svcCancel context.CancelFunc
	started   bool

	listeners []EventListener
	notifier  Notifier // optional: delivers significant events to the user

	// behaviorLog records each periodic-task cycle (run/error + duration) under
	// system:background so a background worker that silently stops running — the
	// recurring production blind spot — shows up as a gap in the log rather than
	// mysterious silence. nil-safe; nil disables background event logging.
	behaviorLog *agentlog.Writer

	// AuroraDream: memory consolidation.
	dreamer          Dreamer
	dreamRunning     bool
	dreamTimerCancel context.CancelFunc // independent dreaming check timer
	// drainStreak counts consecutive near-term re-triggers issued to drain a
	// diary backlog that overflowed the per-cycle byte cap. Reset when a cycle
	// reports no remaining backlog; capped at maxDrainStreak so a bug that keeps
	// reporting backlog cannot pin the dreamer to a 20s hot-loop.
	drainStreak int

	// Periodic tasks (gmail polling, etc.).
	tasks       []PeriodicTask
	taskCancels []context.CancelFunc
	taskStatus  map[string]*TaskStatus

	// stateDir, when set, is the directory where task last-run times are
	// persisted so periodic intervals survive gateway restarts. The
	// auto-deploy timer SIGUSR1s the gateway on every main push, so without
	// persistence every restart would re-run all tasks 30s in — making 24h
	// (boot) and weekly (skill evolution/curator) intervals meaningless.
	stateDir string
}

// NewService creates a new autonomous service (dreaming + periodic tasks).
func NewService(logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	svcCtx, svcCancel := context.WithCancel(context.Background())
	return &Service{
		logger:     logger.With("pkg", "autonomous"),
		svcCtx:     svcCtx,
		svcCancel:  svcCancel,
		taskStatus: make(map[string]*TaskStatus),
	}
}

// RegisterTask adds a periodic task. Must be called before Start().
func (s *Service) RegisterTask(task PeriodicTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	s.taskStatus[task.Name()] = &TaskStatus{Name: task.Name()}
}

// SetStateDir sets the directory for persisting task last-run times across
// restarts. Must be called before Start(). When unset, persistence is disabled
// (in-memory only) and every restart re-runs all tasks after the initial grace.
func (s *Service) SetStateDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateDir = dir
}

// stateFilePath returns the persisted task-state file path, or "" when no state
// dir is configured. Caller must hold s.mu.
func (s *Service) stateFilePathLocked() string {
	if s.stateDir == "" {
		return ""
	}
	return filepath.Join(s.stateDir, "autonomous_state.json")
}

// readPersistedTaskState loads task-name → lastRunAt from path. Missing or
// corrupt files return an empty map (never nil) so callers can merge safely.
func readPersistedTaskState(path string, logger *slog.Logger) map[string]int64 {
	out := map[string]int64{}
	data, err := os.ReadFile(path) //nolint:gosec // state path owned by the service
	if err != nil {
		if !os.IsNotExist(err) && logger != nil {
			logger.Warn("autonomous: failed to read task-state file", "error", err)
		}
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		if logger != nil {
			logger.Warn("autonomous: failed to parse task-state file", "error", err)
		}
		return map[string]int64{}
	}
	if out == nil {
		return map[string]int64{}
	}
	return out
}

// loadStateLocked restores persisted LastRunAt values into taskStatus so
// periodic intervals survive restarts. Called once from Start(). A missing file
// is normal on first boot. Caller must hold s.mu.
func (s *Service) loadStateLocked() {
	path := s.stateFilePathLocked()
	if path == "" {
		return
	}
	persisted := readPersistedTaskState(path, s.logger)
	for name, lastRun := range persisted {
		if st, ok := s.taskStatus[name]; ok {
			st.LastRunAt = lastRun
		}
	}
}

// saveState merges this process's non-zero LastRunAt values into the on-disk
// map and writes atomically. Merge (not replace) is load-bearing: a
// dev/live-test process registers only a subset of tasks but used to share
// ~/.deneb/autonomous_state.json — a full replace wiped prod-only keys
// (judge-accuracy, skill-workout, …) and every subsequent prod restart
// re-fired those lanes after the 30s grace on a cold LLM.
func (s *Service) saveState() {
	s.mu.Lock()
	path := s.stateFilePathLocked()
	if path == "" {
		s.mu.Unlock()
		return
	}
	ours := make(map[string]int64, len(s.taskStatus))
	for name, st := range s.taskStatus {
		if st.LastRunAt > 0 {
			ours[name] = st.LastRunAt
		}
	}
	s.mu.Unlock()

	merged := readPersistedTaskState(path, s.logger)
	for name, lastRun := range ours {
		merged[name] = lastRun
	}
	data, err := json.Marshal(merged)
	if err != nil {
		s.logger.Warn("autonomous: failed to marshal task state", "error", err)
		return
	}
	if err := atomicfile.WriteFile(path, data, &atomicfile.Options{Perm: 0o600}); err != nil {
		s.logger.Warn("autonomous: failed to write task-state file", "error", err)
	}
}

// TaskStatus returns the status of a registered task, or nil if not found.
func (s *Service) TaskStatus(name string) *TaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.taskStatus[name]
	if !ok {
		return nil
	}
	// Return a copy.
	cp := *st
	return &cp
}

// Start initializes the service and starts all registered periodic tasks.
// Dreaming timer is started when SetDreamer is called. Idempotent while loops
// are already running; after Stop() it recreates svcCtx (Stop cancels it) so
// config-reload Stop→Start does not spawn children of a dead context.
func (s *Service) Start() {
	s.mu.Lock()
	if len(s.taskCancels) > 0 {
		s.mu.Unlock()
		return // loops already running
	}
	if s.svcCtx == nil || s.svcCtx.Err() != nil {
		s.svcCtx, s.svcCancel = context.WithCancel(context.Background())
	}
	s.started = true
	s.loadStateLocked() // restore LastRunAt so intervals survive restarts
	tasks := make([]PeriodicTask, len(s.tasks))
	copy(tasks, s.tasks)
	svcCtx := s.svcCtx
	// Restart the dream timer after Stop cancelled it.
	if s.dreamer != nil && s.dreamTimerCancel == nil {
		dctx, dcancel := context.WithCancel(svcCtx)
		s.dreamTimerCancel = dcancel
		go s.dreamTimerLoop(dctx)
	}
	s.mu.Unlock()

	for _, task := range tasks {
		ctx, cancel := context.WithCancel(svcCtx) //nolint:gosec // G118 — cancel stored in s.taskCancels
		s.mu.Lock()
		s.taskCancels = append(s.taskCancels, cancel)
		s.mu.Unlock()
		go s.runTaskLoop(ctx, task)
	}

	s.logger.Info("autonomous service started", "tasks", len(tasks))
}

// Stop shuts down the service and all periodic tasks.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel all periodic task loops.
	for _, cancel := range s.taskCancels {
		cancel()
	}
	s.taskCancels = nil

	if s.dreamTimerCancel != nil {
		s.dreamTimerCancel()
		s.dreamTimerCancel = nil
	}
	// Cancel service-level context to stop any in-flight async operations.
	// Nil out so the next Start() allocates a fresh parent context — leaving a
	// cancelled svcCtx made config-reload Stop→Start spawn immediately-dead loops.
	if s.svcCancel != nil {
		s.svcCancel()
		s.svcCancel = nil
		s.svcCtx = nil
	}
	s.started = false
	s.logger.Info("autonomous service stopped")
}

// OnEvent registers a listener for lifecycle events (dreaming).
func (s *Service) OnEvent(listener EventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// SetNotifier sets the optional notifier for delivering significant events
// to the user (e.g., to the native client).
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// SetDreamer sets the optional dreamer for AuroraDream memory consolidation.
// When set, an independent periodic timer (every 30 min) checks dreaming
// conditions even when the user is inactive.
func (s *Service) SetDreamer(d Dreamer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dreamer = d

	// Start independent dreaming check timer if not already running.
	if d != nil && s.dreamTimerCancel == nil && s.svcCtx != nil {
		ctx, cancel := context.WithCancel(s.svcCtx)
		s.dreamTimerCancel = cancel
		go s.dreamTimerLoop(ctx)
	}
}

// dreamTimerLoop periodically checks dreaming conditions independently of
// user activity. This ensures time-based and data-volume triggers fire
// even when the user is idle.
func (s *Service) dreamTimerLoop(ctx context.Context) {
	// Check once immediately: lastDream is restored from disk, so on a host
	// where auto-deploy restarts the gateway every few minutes the 30-minute
	// ticker keeps resetting and the time trigger can be deferred for hours
	// (observed 2026-07-03: four restarts in twenty minutes). The immediate
	// check is safe — ShouldDream is false inside the 8h interval and
	// runDreamingAsync dedups overlapping cycles.
	s.dreamTimerTick(ctx)
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dreamTimerTick(ctx)
		}
	}
}

// dreamTimerTick runs one dreaming-condition check. Recovery is per-tick so a
// panic in ShouldDream or the async spawn kills one check, not the loop — an
// unguarded panic here would silently end dreaming for the process lifetime.
func (s *Service) dreamTimerTick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("panic in dream timer tick", "panic", r)
		}
	}()

	s.mu.Lock()
	dreamer := s.dreamer
	dreamRunning := s.dreamRunning
	s.mu.Unlock()

	if dreamer != nil && !dreamRunning && dreamer.ShouldDream(ctx) {
		s.runDreamingAsync()
	}
}

// IncrementDreamTurn records a conversation turn and triggers dreaming if conditions are met.
// Called from the chat handler after each agent turn.
func (s *Service) IncrementDreamTurn(ctx context.Context) {
	s.mu.Lock()
	dreamer := s.dreamer
	dreamRunning := s.dreamRunning
	s.mu.Unlock()

	if dreamer == nil {
		s.logger.Debug("aurora-dream: skipping turn increment, dreamer not configured")
		return
	}
	if dreamRunning {
		s.logger.Debug("aurora-dream: skipping turn increment, dream cycle in progress")
		return
	}

	dreamer.IncrementTurn(ctx)

	if dreamer.ShouldDream(ctx) {
		// runDreamingAsync has its own dreamRunning guard under the mutex,
		// so concurrent callers are safely deduplicated there.
		s.runDreamingAsync()
	}
}

// runDreamingAsync launches a dreaming cycle in a background goroutine.
func (s *Service) runDreamingAsync() {
	s.mu.Lock()
	if s.dreamRunning || s.dreamer == nil {
		s.mu.Unlock()
		return
	}
	s.dreamRunning = true
	dreamer := s.dreamer
	svcCtx := s.svcCtx
	s.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				s.logger.Error(
					"aurora-dream: panic recovered",
					"panic", r,
					"stack", string(buf[:n]),
				)
				s.emit(CycleEvent{Type: "dreaming_failed"})
			}
			s.mu.Lock()
			s.dreamRunning = false
			s.mu.Unlock()
		}()

		s.emit(CycleEvent{Type: "dreaming_started"})

		report, err := dreamer.RunDream(svcCtx)
		if err != nil {
			s.logger.Error("aurora-dream: cycle failed", "error", err)
			s.notifyDreaming(nil, err)
			s.emit(CycleEvent{Type: "dreaming_failed"})
			return
		}

		s.logger.Info(
			"aurora-dream: cycle finished",
			"verified", report.FactsVerified,
			"merged", report.FactsMerged,
			"expired", report.FactsExpired,
			"pruned", report.FactsPruned,
			"patterns", report.PatternsExtracted,
			"user_model", report.UserModelUpdated,
			"mutual", report.MutualUpdated,
			"wikiProposed", report.WikiUpdatesProposed,
			"wikiProposalPath", report.WikiProposalPath,
			"durationMs", report.DurationMs,
		)
		s.notifyDreaming(report, nil)
		s.emit(CycleEvent{Type: "dreaming_completed", DreamReport: report})

		// Adaptive backlog drain: a cycle that hit the per-cycle input cap with
		// more diary bytes waiting re-triggers soon instead of waiting the full
		// interval, so a busy backlog consolidates in minutes. A cycle that
		// drained everything resets the streak counter.
		if report.MoreBacklog {
			s.scheduleBacklogDrain()
		} else {
			s.mu.Lock()
			s.drainStreak = 0
			s.mu.Unlock()
		}
	}()
}

// drainRetriggerDelay spaces backlog-drain re-triggers: long enough for the
// completing cycle's dreamRunning flag to reset and to avoid a tight loop, short
// enough that a backlog clears in minutes. A var so tests can shrink it.
// maxDrainStreak bounds the chain so a bug cannot hot-loop the dreamer.
var drainRetriggerDelay = 20 * time.Second

const maxDrainStreak = 30

// scheduleBacklogDrain arms one near-term re-trigger of the dream cycle to
// consume the next backlog chunk. Derives from svcCtx (cancels on shutdown),
// recovers from panics, and self-caps via drainStreak so it cannot hot-loop.
func (s *Service) scheduleBacklogDrain() {
	s.mu.Lock()
	if s.drainStreak >= maxDrainStreak {
		s.logger.Warn("aurora-dream: backlog drain cap reached; waiting for normal cadence", "streak", s.drainStreak)
		s.drainStreak = 0
		s.mu.Unlock()
		return
	}
	s.drainStreak++
	streak := s.drainStreak
	ctx := s.svcCtx
	s.mu.Unlock()
	if ctx == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("aurora-dream: backlog drain re-trigger panic", "panic", r)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-time.After(drainRetriggerDelay):
		}
		s.logger.Info("aurora-dream: draining remaining backlog", "streak", streak)
		s.runDreamingAsync()
	}()
}

// notifyDreaming sends a notification for dreaming cycle results.
func (s *Service) notifyDreaming(report *DreamReport, err error) {
	s.mu.Lock()
	notifier := s.notifier
	s.mu.Unlock()
	if notifier == nil {
		return
	}

	ctx, cancel := context.WithTimeout(s.svcCtx, 15*time.Second)
	defer cancel()

	var msg string
	if err != nil {
		msg = fmt.Sprintf("⚠️ Aurora Dream 실패: %s", truncateOutput(err.Error(), 100))
	} else if report != nil {
		total := report.FactsVerified + report.FactsMerged + report.FactsExpired +
			report.FactsPruned + report.PatternsExtracted +
			report.UserModelUpdated + report.MutualUpdated +
			report.WikiPagesCreated + report.WikiPagesUpdated +
			report.WikiProjectDigests
		dur := float64(report.DurationMs) / 1000
		// A phase error makes the headline say 실패 / 부분 완료 — otherwise the card
		// title read "완료: 변경 없음" while the real failure sat in the summary, so a
		// dream that errored every cycle looked like a quiet no-op (see #… synthesis
		// parse failures masked as "변경 없음").
		failed := len(report.PhaseErrors) > 0
		switch {
		case total == 0 && failed:
			// 0 changes *because* a phase failed — surface the failure, not "변경 없음".
			msg = fmt.Sprintf("⚠️ Aurora Dream 실패 (%.1fs)", dur)
		case total == 0:
			msg = fmt.Sprintf("🌙 Aurora Dream 완료: 변경 없음 (%.1fs)", dur)
		case report.WikiPagesCreated > 0 || report.WikiPagesUpdated > 0 || report.WikiUpdatesProposed > 0 || report.WikiProjectDigests > 0:
			// Wiki dreaming report.
			head := "📖 Wiki Dream 완료"
			if failed {
				head = "⚠️ Wiki Dream 부분 완료"
			}
			msg = fmt.Sprintf("%s: 제안 %d, 생성 %d, 수정 %d (%.1fs)",
				head, report.WikiUpdatesProposed, report.WikiPagesCreated, report.WikiPagesUpdated, dur)
			if report.WikiProjectDigests > 0 {
				msg += fmt.Sprintf(", 근황 %d", report.WikiProjectDigests)
			}
			// The 사용자-category subset of created/updated — surfaced so the
			// operator sees their own model evolving, not just page counts.
			if report.UserModelUpdated > 0 {
				msg += fmt.Sprintf(", 사용자모델 %d", report.UserModelUpdated)
			}
		default:
			head := "🌙 Aurora Dream 완료"
			if failed {
				head = "⚠️ Aurora Dream 부분 완료"
			}
			msg = fmt.Sprintf("%s: 검증 %d, 병합 %d, 만료 %d, 정리 %d, 패턴 %d, 프로필 %d, 관계 %d (%.1fs)",
				head, report.FactsVerified, report.FactsMerged, report.FactsExpired,
				report.FactsPruned, report.PatternsExtracted,
				report.UserModelUpdated, report.MutualUpdated, dur)
		}
		if len(report.VerifyFindings) > 0 {
			msg += fmt.Sprintf("\n🔍 새 검증 발견 %d건:", len(report.VerifyFindings))
			for _, f := range report.VerifyFindings {
				msg += "\n  - " + truncateOutput(f, 80)
			}
		}
		if report.StaleDuesClosed > 0 || len(report.StaleDueAlert) > 0 {
			if report.StaleDuesClosed > 0 {
				msg += fmt.Sprintf("\n📅 지난 기한 해제 %d건:", report.StaleDuesClosed)
			} else {
				msg += "\n📅 지난 기한:"
			}
			for _, a := range report.StaleDueAlert {
				msg += "\n  - " + truncateOutput(a, 80)
			}
		}
		if len(report.RecallDemandTerms) > 0 {
			msg += "\n❓ 답 못한 질문 주제: " + strings.Join(report.RecallDemandTerms, ", ")
		}
		if len(report.VerifyPending) > 0 {
			msg += fmt.Sprintf("\n🙋 확인 부탁 %d건 (자동으로 못 고칩니다 — 작업 피드에서 \"확인했음\"):", len(report.VerifyPending))
			for _, p := range report.VerifyPending {
				msg += "\n  - " + truncateOutput(p.Detail, 80)
			}
		}
		if report.VerifyFindingsRepeat > 0 {
			msg += fmt.Sprintf("\n🔍 기존 발견 %d건 유지 (미해결)", report.VerifyFindingsRepeat)
		}
		// What exactly changed + how to roll it back (wiki git snapshot).
		if report.WikiChangeSummary != "" {
			msg += "\n" + report.WikiChangeSummary
		}
		if len(report.PhaseErrors) > 0 {
			msg += fmt.Sprintf("\n⚠️ 실패: %s", strings.Join(report.PhaseErrors, "; "))
		}
	}
	if msg != "" {
		if notifyErr := notifier.Notify(ctx, msg); notifyErr != nil {
			s.logger.Warn("aurora-dream: notification failed", "error", notifyErr)
		}
	}
}

// initialGrace is the default delay before a task's first run, giving the
// gateway time to finish booting.
const initialGrace = 30 * time.Second

// computeInitialDelay returns how long runTaskLoop waits before the first run.
// Default is the grace period, but a task that ran recently (LastRunAt restored
// from disk across a restart) waits out only the remainder of its interval — so
// the auto-deploy SIGUSR1 storm cannot re-fire 24h/weekly tasks on every
// restart. An overdue or never-run task uses the grace period.
func computeInitialDelay(lastRunAt int64, interval, grace time.Duration, now time.Time) time.Duration {
	if lastRunAt <= 0 {
		return grace
	}
	elapsed := now.Sub(time.UnixMilli(lastRunAt))
	if elapsed >= interval {
		return grace
	}
	if remaining := interval - elapsed; remaining > grace {
		return remaining
	}
	return grace
}

// runTaskLoop runs a periodic task with panic recovery and status tracking.
func (s *Service) runTaskLoop(ctx context.Context, task PeriodicTask) {
	name := task.Name()
	interval := task.Interval()
	s.logger.Debug("periodic task started", "task", name, "interval", interval)

	// First-run delay: the grace period, or the remainder of the interval when
	// the task ran recently (LastRunAt persisted across a restart).
	initialDelay := initialGrace
	if st := s.TaskStatus(name); st != nil {
		initialDelay = computeInitialDelay(st.LastRunAt, interval, initialGrace, time.Now())
	}
	initialTimer := time.NewTimer(initialDelay)
	defer initialTimer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runOnce := func() {
		s.executeTask(ctx, task)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("periodic task stopped", "task", name)
			return
		case <-initialTimer.C:
			runOnce()
		case <-ticker.C:
			runOnce()
		}
	}
}

// executeTask runs a single task cycle with panic recovery and status bookkeeping.
func (s *Service) executeTask(ctx context.Context, task PeriodicTask) {
	name := task.Name()

	// Mark running.
	s.mu.Lock()
	st := s.taskStatus[name]
	if st != nil && st.Running {
		s.mu.Unlock()
		s.logger.Debug("periodic task still running, skipping", "task", name)
		return
	}
	if st != nil {
		st.Running = true
	}
	s.mu.Unlock()

	defer func() {
		// Panic recovery.
		if r := recover(); r != nil {
			s.logger.Error("periodic task panic recovered", "task", name, "panic", r)
			s.mu.Lock()
			if st != nil {
				st.Running = false
				st.ErrorCount++
				st.LastError = fmt.Sprintf("panic: %v", r)
			}
			s.mu.Unlock()
		}
	}()

	start := time.Now()
	err := task.Run(ctx)
	elapsed := time.Since(start)

	s.mu.Lock()
	if st != nil {
		st.Running = false
		st.RunCount++
		st.LastRunAt = time.Now().UnixMilli()
		if err != nil {
			st.ErrorCount++
			st.LastError = err.Error()
		} else {
			st.LastError = ""
		}
	}
	s.mu.Unlock()

	// Persist LastRunAt so this run is remembered across a restart.
	s.saveState()

	// Behavioral event: record task cycles (gmail poll, evolution, curator,
	// heartbeat, boot) under system:background so a worker that silently stops
	// running shows up as a gap in the log instead of mysterious silence.
	// Quiet no-op ticks of high-frequency tasks are the one exception:
	// goal-loop's ~2min ticks alone wrote 2,200 ok/0ms records in 3 prod days,
	// drowning the stream. Their liveness is still covered by saveState above
	// (autonomous_state.json, mtime-watched by the observatory); errors and
	// cycles that did real work (≥1s) always land here. behaviorLog is nil-safe.
	outcome := "ok"
	errStr := ""
	if err != nil {
		outcome = "error"
		errStr = err.Error()
	}
	quietTick := err == nil && elapsed < time.Second && task.Interval() < 30*time.Minute
	if !quietTick {
		agentlog.LogTyped(s.behaviorLog, agentlog.SessionBackground, agentlog.TypeBackgroundJob, agentlog.BackgroundJobData{
			Kind:       "autonomous",
			Name:       name,
			Outcome:    outcome,
			DurationMs: elapsed.Milliseconds(),
			Error:      errStr,
		})
	}

	if err != nil {
		s.logger.Warn("periodic task failed", "task", name, "error", err)
	}
}

// SetBehaviorLog wires the behavioral event log so each periodic-task cycle is
// recorded under system:background. Optional; nil disables background logging.
func (s *Service) SetBehaviorLog(w *agentlog.Writer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.behaviorLog = w
}

func (s *Service) emit(event CycleEvent) {
	event.Ts = time.Now().UnixMilli()
	// Mirror the dreaming lifecycle (completed/failed) into the behavioral event
	// log so memory-consolidation cycles are observable next to the periodic
	// tasks — "aurora-dream ran 0 times this week" should show as a gap in the
	// log rather than be inferred from silence. "started" is omitted as noise.
	// behaviorLog is nil-safe.
	if event.Type == "dreaming_completed" || event.Type == "dreaming_failed" {
		outcome := "ok"
		if event.Type == "dreaming_failed" {
			outcome = "error"
		}
		agentlog.LogTyped(s.behaviorLog, agentlog.SessionBackground, agentlog.TypeBackgroundJob, agentlog.BackgroundJobData{
			Kind:    "autonomous",
			Name:    "aurora-dream",
			Outcome: outcome,
		})
	}
	s.mu.Lock()
	listeners := make([]EventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("aurora-dream: event listener panic", "event", event.Type, "panic", r)
				}
			}()
			l(event)
		}()
	}
}

// truncateOutput truncates a string to maxRunes runes, preserving UTF-8 boundaries.
func truncateOutput(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
