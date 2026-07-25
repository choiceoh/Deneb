package genesis

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	genesiseprocess "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/eprocess"
)

// stashWithObservations resolves a watch after n clean observations and returns
// whatever the tracker logged.
func stashWithObservations(t *testing.T, baseline float64, n int) (string, *rollbackBaselineTest) {
	t.Helper()
	var buf bytes.Buffer
	tr := newTestTracker(t)
	tr.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	w := &evolveWatch{ep: genesiseprocess.NewEProcess(genesiseprocess.DefaultEProcessAlpha, baseline)}
	for i := 0; i < n; i++ {
		w.ep.Observe(false)
	}
	tr.mu.Lock()
	tr.stashBaselineTestLocked("sk", w)
	got := tr.pendingBaselineTest["sk"]
	tr.mu.Unlock()
	return buf.String(), got
}

// A label the e-process could never have rejected is worthless but silent: it
// is written, counted as UnfairLabels, and advances the ladder by nothing.
// Live 2026-07-25 that produced weeks of "라벨 0/20 · 축적 중" while the input
// was structurally short — the warning is what makes the next one obvious.
func TestStashBaselineTestWarnsWhenRejectWasUnreachable(t *testing.T) {
	logged, got := stashWithObservations(t, 0.05, 6)
	if got.RejectReachable {
		t.Fatalf("N=6 against a baseline of 0.05 must not be reachable")
	}
	if !strings.Contains(logged, "below reject-reachability") {
		t.Fatalf("an unusable label must warn, got %q", logged)
	}
	for _, want := range []string{"observations=6", "needed=8", "DENEB_SKILL_WATCH_MAX_AGE_DAYS"} {
		if !strings.Contains(logged, want) {
			t.Errorf("warning must carry %q so it is actionable, got %q", want, logged)
		}
	}
}

// A usable label must stay quiet — a warning on every resolution would be
// noise and would train the reader to ignore it.
func TestStashBaselineTestStaysQuietWhenRejectWasReachable(t *testing.T) {
	logged, got := stashWithObservations(t, 0.05, 12)
	if !got.RejectReachable {
		t.Fatalf("N=12 against a baseline of 0.05 must be reachable")
	}
	if strings.Contains(logged, "below reject-reachability") {
		t.Fatalf("a usable label must not warn, got %q", logged)
	}
}
