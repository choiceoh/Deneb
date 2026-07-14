package genesis

import (
	"context"
	"io"
	"log/slog"
	"testing"

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
	}, tracker
}

func errLine(msg string, ts int64) observe.LogLine {
	return observe.LogLine{Ts: ts, Level: "error", Msg: msg}
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

	// Second run over the same ring → dedup, no new candidate.
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, _ := tracker.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 50)
	if len(again) != 1 {
		t.Fatalf("dedup failed: %d candidates after re-run", len(again))
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
