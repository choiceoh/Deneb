package genesis

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func TestNormalizeErrorSignature(t *testing.T) {
	// Same defect, different ids/numbers/quoted values → one signature.
	a := normalizeErrorSignature(`failed to load skill "topsolar-db" (attempt 3) id=0xdeadbeef12`)
	b := normalizeErrorSignature(`failed to load skill "procurement" (attempt 17) id=0xfeed9900ab`)
	if a != b {
		t.Fatalf("variable parts not collapsed:\n a=%q\n b=%q", a, b)
	}
	if a == "" {
		t.Fatal("signature collapsed to empty")
	}
}

func TestIsExternalFaultReturnsTrueForNetworkAndRateLimitFalseForCodeDefects(t *testing.T) {
	ext := []string{
		"request failed with 429 rate limit",
		"read tcp: connection reset by peer",
		"context deadline exceeded",
		"unexpected EOF",
		"wormhole_error: upstream unreachable: glm-5.2",
		"page.goto: net::ERR_NAME_NOT_RESOLVED at tsgw.topsolar.kr",
		"lookup oauth2.googleapis.com: server misbehaving",
	}
	for _, m := range ext {
		if !isExternalFault(observe.LogLine{Msg: m}) {
			t.Fatalf("expected external fault, got code-relevant: %q", m)
		}
	}
	// A genuine code defect must NOT be filtered out.
	if isExternalFault(observe.LogLine{Msg: "nil pointer dereference in reportHandler"}) {
		t.Fatal("a real code error was misclassified as external")
	}
	// External cause carried in the error attr is also filtered.
	if !isExternalFault(observe.LogLine{Msg: "llm call failed", Attrs: map[string]string{"error": "API error 429"}}) {
		t.Fatal("external fault in error= attr not filtered")
	}
}

func newMiningTask(t *testing.T, lines []observe.LogLine) (*RuntimeErrorMiningTask, *Tracker) {
	t.Helper()
	tracker := newTestTracker(t)
	return &RuntimeErrorMiningTask{
		ErrorLines: func(int) []observe.LogLine { return lines },
		Tracker:    tracker,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Isolated rolling state — the default resolves to the REAL
		// ~/.deneb/data, which tests must never touch (and whose watermark
		// would leak between tests).
		StatePath: filepath.Join(t.TempDir(), "runtime_error_signature_state.json"),
		// These tests exercise fold+mine together; the fold/mine cadence
		// split has its own test.
		MiningInterval: time.Nanosecond,
	}, tracker
}

// Folds run at the task cadence but authoring is throttled to the mining
// cadence: a run inside the throttle window still folds new lines into the
// rolling window (so hot-swap ring wipes lose at most one fold interval),
// without authoring candidates.
func TestRuntimeErrorMining_FoldsWithoutAuthoringInsideMiningThrottle(t *testing.T) {
	var first []observe.LogLine
	for i := 0; i < 2; i++ {
		first = append(first, errLine("slice oob in composer idx=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, first)
	task.MiningInterval = 12 * time.Hour
	if err := task.Run(context.Background()); err != nil { // run 1: mines (nothing over floor yet)
		t.Fatal(err)
	}

	var second []observe.LogLine
	for i := 0; i < 4; i++ { // 2+4 = 6 ≥ floor 5
		second = append(second, errLine("slice oob in composer idx=", int64(2000+i)))
	}
	task.ErrorLines = func(int) []observe.LogLine { return second }
	if err := task.Run(context.Background()); err != nil { // run 2: inside throttle → fold only
		t.Fatal(err)
	}
	if got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50); len(got) != 0 {
		t.Fatalf("throttled run must not author, got %d candidates", len(got))
	}
	st := loadRuntimeErrorState(task.StatePath)
	if e := st.Sigs[normalizeErrorSignature("slice oob in composer idx=")]; e == nil || e.Count != 6 {
		t.Fatalf("throttled run must still fold: %+v", st.Sigs)
	}

	task.MiningInterval = time.Nanosecond // throttle elapsed
	task.ErrorLines = func(int) []observe.LogLine { return nil }
	if err := task.Run(context.Background()); err != nil { // run 3: mines the folded window
		t.Fatal(err)
	}
	if got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50); len(got) != 1 {
		t.Fatalf("post-throttle run must author from the folded window, got %d", len(got))
	}
}

// testLineBase anchors synthetic line timestamps near NOW: the rolling
// window prunes by a 7d TTL against the wall clock, so raw epoch-1970
// offsets would age out instantly.
var testLineBase = time.Now().Add(-time.Hour).UnixMilli()

func errLine(msg string, ts int64) observe.LogLine {
	return observe.LogLine{Ts: testLineBase + ts, Level: "error", Msg: msg}
}

func warnLine(msg string, ts int64) observe.LogLine {
	return observe.LogLine{Ts: testLineBase + ts, Level: "warn", Msg: msg}
}

func TestRuntimeErrorMining_RecurringCodeErrorBecomesCandidate(t *testing.T) {
	var lines []observe.LogLine
	// 6× a recurring code defect (>= threshold 5) with varying ids.
	for i := 0; i < 6; i++ {
		lines = append(lines, errLine("nil pointer dereference in reportHandler seq=", int64(1000+i)))
	}
	// 3× external fault (excluded regardless of count).
	for i := 0; i < 3; i++ {
		lines = append(lines, errLine("upstream returned 429 rate limit", int64(2000+i)))
	}
	// 2× a one-off code error (below threshold).
	lines = append(lines, errLine("rare parse glitch A", 3000), errLine("rare parse glitch A", 3001))

	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate (the recurring code error), got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Scope != "code" {
		t.Fatalf("candidate scope = %q, want code", c.Scope)
	}
	if got := c.Source; len(got) < len(runtimeErrorSourcePrefix) || got[:len(runtimeErrorSourcePrefix)] != runtimeErrorSourcePrefix {
		t.Fatalf("candidate source = %q, want %s prefix", c.Source, runtimeErrorSourcePrefix)
	}
	// Every authored candidate carries the self-measurable usefulness contract
	// (quiet-hours after the fix) — landed dispatches without one were
	// invisible to effect judgment (확인 0 · 대기 1 on 2026-07-20).
	if c.ImpactContract == nil ||
		c.ImpactContract.Direction != selfCorrectionImpactDirectionIncrease ||
		c.ImpactContract.Target != runtimeErrorImpactQuietTargetHours {
		t.Fatalf("candidate must carry the quiet-hours impact contract: %+v", c.ImpactContract)
	}
	// Korean operator text, same two invariants: never prescribe silencing the
	// miner by log level, always allow declining an external root cause.
	if strings.Contains(c.ProposedChange, "로그 레벨을 낮춰서 해결") {
		t.Fatalf("log-level downgrade must not be the prescribed escape hatch: %q", c.ProposedChange)
	}
	if !strings.Contains(c.ProposedChange, "아무것도 반영하지 말고") {
		t.Fatalf("expected/external defects must decline, not patch the logger: %q", c.ProposedChange)
	}

	// Second run over the same ring → dedup, no new candidate.
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if len(again) != 1 {
		t.Fatalf("dedup failed: %d candidates after re-run", len(again))
	}
}

// TestRuntimeErrorMining_StaleSignaturesHeldFromAuthoring guards the
// freshness gate: a signature that recurred past the floor but stopped firing
// (self-resolved burst — live 2026-07-20: a fixed wormhole/kimi outage burned
// 2 dispatch slots on honest declines) must not author a candidate. The 7d
// TTL still keeps it as evidence.
func TestRuntimeErrorMining_StaleSignaturesHeldFromAuthoring(t *testing.T) {
	staleBase := time.Now().Add(-3 * 24 * time.Hour).UnixMilli()
	var lines []observe.LogLine
	for i := 0; i < 6; i++ { // recurring but quiet for 3d
		lines = append(lines, observe.LogLine{Ts: staleBase + int64(i), Level: "error", Msg: "stale burst defect in xHandler"})
	}
	for i := 0; i < 6; i++ { // recurring AND still firing
		lines = append(lines, errLine("live defect in yHandler seq=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only the live signature to author, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Title, "live defect") {
		t.Fatalf("wrong signature authored: %q", got[0].Title)
	}
	if e := loadRuntimeErrorState(task.StatePath).Sigs[normalizeErrorSignature("stale burst defect in xHandler")]; e == nil {
		t.Fatal("stale signature must stay in the rolling window as evidence")
	}
}

// TestRuntimeErrorMining_SelfMeasuresImpactAfterWatchPass closes problem 7's
// loop in miniature: author (contract attached) → accept → deliver to
// watch_passed → the next mining run measures quiet-hours from the rolling
// window and records the usefulness verdict itself.
func TestRuntimeErrorMining_SelfMeasuresImpactAfterWatchPass(t *testing.T) {
	prevWindow := runtimeErrorImpactObservationWindow
	runtimeErrorImpactObservationWindow = 0
	defer func() { runtimeErrorImpactObservationWindow = prevWindow }()

	var lines []observe.LogLine
	for i := 0; i < 6; i++ {
		lines = append(lines, errLine("nil deref in fooHandler seq=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected 1 authored candidate (err=%v), got %d", err, len(got))
	}
	cand := got[0]
	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: cand.ID, Status: SelfCorrectionStatusAccepted, Reviewer: "test",
	}); err != nil {
		t.Fatal(err)
	}
	watchPassImpactCandidate(t, tracker, cand.ID, "attempt-1")

	// The fix landed and the signature went quiet for 72h (> target 48h) WHILE
	// the error stream kept running. The live sibling is what makes the quiet
	// attributable: with every signature silent the gateway merely went idle,
	// and that case is asserted separately below.
	st := loadRuntimeErrorState(task.StatePath)
	for _, e := range st.Sigs {
		e.LastAt = time.Now().Add(-72 * time.Hour).UnixMilli()
	}
	st.Sigs["unrelated noisy signature"] = &runtimeErrorSigEntry{
		Count: 5, FirstAt: time.Now().Add(-96 * time.Hour).UnixMilli(),
		LastAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}
	if err := saveRuntimeErrorState(task.StatePath, st); err != nil {
		t.Fatal(err)
	}

	task.ErrorLines = func(int) []observe.LogLine { return nil }
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := tracker.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected the single candidate back (err=%v), got %d", err, len(rows))
	}
	if rows[0].ImpactResult == nil {
		t.Fatalf("mining run must record the impact verdict itself: %+v", rows[0])
	}
	if rows[0].ImpactResult.Status != selfCorrectionImpactVerified {
		t.Fatalf("quiet 72h >= target 48h must verify, got %q", rows[0].ImpactResult.Status)
	}
}

// The confound the sibling control exists to kill: if EVERY signature is quiet,
// the stream (not the defect) stopped, and a verdict either way would be
// measuring our own downtime. The candidate must stay pending for a later run.
func TestRuntimeErrorMining_LeavesImpactPendingWhenWholeStreamIsQuiet(t *testing.T) {
	prevWindow := runtimeErrorImpactObservationWindow
	runtimeErrorImpactObservationWindow = 0
	defer func() { runtimeErrorImpactObservationWindow = prevWindow }()

	var lines []observe.LogLine
	for i := 0; i < 6; i++ {
		lines = append(lines, errLine("nil deref in fooHandler seq=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("expected 1 authored candidate (err=%v), got %d", err, len(got))
	}
	cand := got[0]
	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: cand.ID, Status: SelfCorrectionStatusAccepted, Reviewer: "test",
	}); err != nil {
		t.Fatal(err)
	}
	watchPassImpactCandidate(t, tracker, cand.ID, "attempt-1")

	st := loadRuntimeErrorState(task.StatePath)
	for _, e := range st.Sigs {
		e.LastAt = time.Now().Add(-72 * time.Hour).UnixMilli()
	}
	if err := saveRuntimeErrorState(task.StatePath, st); err != nil {
		t.Fatal(err)
	}

	task.ErrorLines = func(int) []observe.LogLine { return nil }
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := tracker.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected the single candidate back (err=%v), got %d", err, len(rows))
	}
	// The measurement now RECORDS that it could not attribute, rather than
	// staying silent: the ledger shows why this candidate has no usefulness
	// answer, and the miner re-measures it once the stream is live again.
	if rows[0].ImpactResult == nil {
		t.Fatal("an unattributable measurement must still be recorded")
	}
	if rows[0].ImpactResult.Status != selfCorrectionImpactInconclusive {
		t.Fatalf("dead stream must read inconclusive, got %q", rows[0].ImpactResult.Status)
	}
	if rows[0].ImpactResult.Samples != 0 {
		t.Errorf("no control means zero samples, got %d", rows[0].ImpactResult.Samples)
	}
}

func TestRuntimeErrorMining_PerRunCap(t *testing.T) {
	var lines []observe.LogLine
	// Three distinct recurring signatures, each >= threshold.
	for _, base := range []string{"defect alpha in aHandler", "defect beta in bHandler", "defect gamma in cHandler"} {
		for i := 0; i < 5; i++ {
			lines = append(lines, errLine(base, int64(100+i)))
		}
	}
	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if len(got) != runtimeErrorMaxCandidatesPerRun {
		t.Fatalf("per-run cap not honored: got %d, want %d", len(got), runtimeErrorMaxCandidatesPerRun)
	}
}

// The window survives restarts: two runs each seeing only PART of the
// recurrence (a hot-swap wiped the ring in between) still accumulate to the
// floor. This is the exact failure mode that starved the lane all week —
// snapshot mining over an in-memory ring on an active-deploy day.
func TestRuntimeErrorMining_WindowSurvivesRestart(t *testing.T) {
	var first []observe.LogLine
	for i := 0; i < 3; i++ {
		first = append(first, errLine("panic in feedRenderer col=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, first)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50); len(got) != 0 {
		t.Fatalf("3 < floor 5: no candidate expected yet, got %d", len(got))
	}

	// "Restart": fresh ring with only 2 more occurrences (new timestamps).
	var second []observe.LogLine
	for i := 0; i < 2; i++ {
		second = append(second, errLine("panic in feedRenderer col=", int64(2000+i)))
	}
	task.ErrorLines = func(int) []observe.LogLine { return second }
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if len(got) != 1 {
		t.Fatalf("3+2 across restarts must reach floor 5, got %d candidates", len(got))
	}
}

// The watermark deduplicates: re-feeding the same ring lines (same
// timestamps) must not double-count.
func TestRuntimeErrorMining_WatermarkDeduplicatesAcrossRuns(t *testing.T) {
	var lines []observe.LogLine
	for i := 0; i < 3; i++ {
		lines = append(lines, errLine("index oob in sorter idx=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, lines)
	for run := 0; run < 3; run++ { // same lines three times = still count 3
		if err := task.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50); len(got) != 0 {
		t.Fatalf("re-fed lines double-counted into a candidate: %d", len(got))
	}
}

// Warn-only signatures use the stricter floor; crossing it promotes.
func TestRuntimeErrorMining_WarnFloorStricterThanError(t *testing.T) {
	var lines []observe.LogLine
	// 11 warns: below the warn floor (12), above the error floor (5).
	for i := 0; i < 11; i++ {
		lines = append(lines, warnLine("model failed, trying fallback chain step=", int64(1000+i)))
	}
	task, tracker := newMiningTask(t, lines)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50); len(got) != 0 {
		t.Fatalf("11 warns < warn floor 12: no candidate expected, got %d", len(got))
	}
	// One more warn crosses the warn floor.
	task.ErrorLines = func(int) []observe.LogLine {
		return []observe.LogLine{warnLine("model failed, trying fallback chain step=", 5000)}
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if len(got) != 1 {
		t.Fatalf("12 warns must promote, got %d", len(got))
	}
}

// Aged-out signatures fall off the rolling window.
func TestRuntimeErrorMiningStatePrunesAgedSignatures(t *testing.T) {
	st := &runtimeErrorState{Sigs: map[string]*runtimeErrorSigEntry{}}
	old := time.Now().Add(-8 * 24 * time.Hour).UnixMilli()
	st.Sigs["stale sig"] = &runtimeErrorSigEntry{Count: 20, LastAt: old}
	st.fold(nil, time.Now())
	if _, ok := st.Sigs["stale sig"]; ok {
		t.Fatal("signature older than the 7d TTL not pruned")
	}
}
