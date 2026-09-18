package genesis

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// An observe-only line has no fix by construction, so a candidate built from
// one can only be declined or land with no effect — either way it burns one of
// the day's scarce dispatch slots.
func TestIsObserveOnlySignalMatchesExplicitAuthorMarkers(t *testing.T) {
	advisory := []string{
		"regression-watch: regression detected (observe-only)",
		"regression-watch: regression detected (observe only)",
		"context truncated without summaries (bootstrap will recover)",
		"health ratchet regressed — advisory, not a gate",
		"deadcode miner dry-run produced 3 findings",
		"cache invalidation was a no-op this cycle",
		"tool complete",
	}
	for _, msg := range advisory {
		if !isObserveOnlySignal(observe.LogLine{Msg: msg}) {
			t.Errorf("must be treated as observe-only: %q", msg)
		}
	}
}

// The corpus below is not invented: every entry is a signature that actually
// reached the coding lane and came back declined with "이것은 의도된 동작"
// (self_correction_candidates.jsonl, 2026-08..09). Each emit site was given the
// author marker instead of widening the miner's inference, so the same line can
// never buy another dispatch slot. Keep this list in step with those sites.
func TestIsObserveOnlySignalCoversMarkedProductionSignals(t *testing.T) {
	marked := []string{
		"skipping metered fallback candidate (advisory)",
		"run budget exhausted mid-work; resuming no-tools fallback from checkpoint (advisory)",
		"role points at a metered model; fallback chains will skip it and may end without a candidate (advisory)",
		"agent soft deadline reached; forcing no-tools wrap-up (advisory)",
		"stream interrupted, retrying turn on same model (advisory)",
		"local serving engine is refusing requests; its models go straight to fallback (advisory)",
		"prompt-plane: tier-1 메모리 블록 축소 (advisory)",
		"mail analysis: skipping unhealthy stage-2 model (advisory)",
		"mail→deal: 거래 조건 인용 검증 실패로 드롭 (advisory)",
		"결재→원장: 비용 인용 검증 실패로 드롭 (advisory)",
		"결재→원장: 품목 행 인용 검증 실패로 드롭 (advisory)",
	}
	for _, msg := range marked {
		if !isObserveOnlySignal(observe.LogLine{Msg: msg}) {
			t.Errorf("marked production signal must be excluded from mining: %q", msg)
		}
	}
}

// The sibling of the corpus above: the same guards have failure modes that are
// NOT intended, and marking a whole family would hide them. A checkpoint the
// fallback cannot use, and a tier-1 block dropped outright, are real losses;
// `replyFn=nil` is the exact class of silent no-answer event logging.md says
// must never be buried.
func TestIsObserveOnlySignalKeepsTheUnmarkedSiblingsMineable(t *testing.T) {
	mineable := []string{
		"run budget exhausted mid-work; checkpoint unavailable",
		"prompt-plane: tier-1 메모리 블록 누락",
		"run started without ReplyFunc in callbacks; in-loop message tool will fail with replyFn=nil",
	}
	for _, msg := range mineable {
		if isObserveOnlySignal(observe.LogLine{Msg: msg}) {
			t.Errorf("unintended sibling must stay mineable: %q", msg)
		}
	}
}

// The narrowness IS the design. Admitting WARN lines is deliberate — graceful
// degradation downgrades real defects — so "a mechanism reported that it
// worked" must stay mineable. Only an explicit marker gets excluded; inferring
// intent from the shape of the message would gut the warn lane.
func TestIsObserveOnlySignalLeavesRealDefectSignalsMineable(t *testing.T) {
	mineable := []string{
		"model failed, trying fallback",
		"model circuit open; skipping straight to fallback chain",
		"polaris: post-compaction safety trim fired",
		"untrusted-tool gate: turn tainted by promptware signal",
		"pipeline: slow prep",
		"mem pressure",
		"periodic task failed",
		"wiki-dream: skipped malformed update item",
	}
	for _, msg := range mineable {
		if isObserveOnlySignal(observe.LogLine{Msg: msg}) {
			t.Errorf("must stay mineable (no explicit marker): %q", msg)
		}
	}
}

// The marker lives in the message the author wrote. An `error` attr carries the
// underlying failure text, where these words would be coincidental — matching
// there would silence real defects whose error string happens to say "timeout
// advisory" or similar.
func TestIsObserveOnlySignalIgnoresErrorAttr(t *testing.T) {
	line := observe.LogLine{
		Msg:   "periodic task failed",
		Attrs: map[string]string{"error": "advisory lock not acquired"},
	}
	if isObserveOnlySignal(line) {
		t.Fatal("the marker must be read from the message, not the error attr")
	}
}

// End-to-end through fold: an advisory line must not even create a signature,
// so it can never accumulate toward a recurrence floor.
func TestFoldSkipsObserveOnlyLines(t *testing.T) {
	st := &runtimeErrorState{Sigs: map[string]*runtimeErrorSigEntry{}}
	lines := []observe.LogLine{
		{Ts: 1000, Level: "WARN", Msg: "regression-watch: regression detected (observe-only)"},
		{Ts: 1001, Level: "WARN", Msg: "regression-watch: regression detected (observe-only)"},
		{Ts: 1002, Level: "ERROR", Msg: "periodic task failed"},
	}
	folded := st.fold(lines, time.UnixMilli(1002))
	if folded != 1 {
		t.Fatalf("folded = %d, want 1 (only the real defect)", folded)
	}
	for sig := range st.Sigs {
		if isObserveOnlySignal(observe.LogLine{Msg: sig}) {
			t.Fatalf("observe-only signature reached the rolling state: %q", sig)
		}
	}
	if len(st.Sigs) != 1 {
		t.Fatalf("signatures = %d, want 1", len(st.Sigs))
	}
}
