package heartbeat

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func fixtureTask(t *testing.T) *heartbeatTask {
	t.Helper()
	return &heartbeatTask{
		homeDir: t.TempDir(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// A recorded firing keeps the variable inputs separate from the contract,
// hashes the contract for drift filtering, and truncates oversized bodies.
func TestRecordHeartbeatFixture_ShapeAndTruncation(t *testing.T) {
	task := fixtureTask(t)
	task.recordHeartbeatFixture(heartbeatFixture{
		SessionKey:    "client:main",
		SweepNudge:    "[자가개선 스윕] ...",
		HeartbeatMD:   "## Active Tasks\n- check builds",
		OutcomeText:   "NO_REPLY",
		SignalSummary: strings.Repeat("가", heartbeatFixtureBodyLimit+50),
	})

	entries, err := jsonlstore.Load[heartbeatFixture](task.heartbeatFixturePath())
	if err != nil || len(entries) != 1 {
		t.Fatalf("want 1 fixture, got %d (err=%v)", len(entries), err)
	}
	got := entries[0]
	if got.FiredAt == 0 || got.HeartbeatHash == "" {
		t.Fatalf("fixture must stamp FiredAt and contract hash: %+v", got)
	}
	if got.OutcomeText != "NO_REPLY" || got.SweepNudge == "" {
		t.Fatalf("fixture must keep outcome and nudges: %+v", got)
	}
	if !strings.HasSuffix(got.SignalSummary, "…[truncated]") {
		t.Fatalf("oversized field should be truncated, got %d runes", len([]rune(got.SignalSummary)))
	}

	// Same contract → same hash; changed contract → different hash.
	task.recordHeartbeatFixture(heartbeatFixture{HeartbeatMD: "## Active Tasks\n- check builds", OutcomeText: "x"})
	task.recordHeartbeatFixture(heartbeatFixture{HeartbeatMD: "## Active Tasks\n- something else", OutcomeText: "x"})
	entries, _ = jsonlstore.Load[heartbeatFixture](task.heartbeatFixturePath())
	if entries[0].HeartbeatHash != entries[1].HeartbeatHash || entries[1].HeartbeatHash == entries[2].HeartbeatHash {
		t.Fatalf("contract hash should track content: %+v", entries)
	}

	// homeDir unset → silent no-op (lane unwired in tests/minimal servers).
	bare := &heartbeatTask{logger: slog.Default()}
	bare.recordHeartbeatFixture(heartbeatFixture{OutcomeText: "x"})
}

// The corpus is a rolling window: past cap+slack the file rewrites to the
// newest cap entries, preserving append order.
func TestRecordHeartbeatFixtureRollingPrunePreservesOrder(t *testing.T) {
	task := fixtureTask(t)
	total := heartbeatFixtureCap + heartbeatFixturePruneSlack + 1
	for i := 0; i < total; i++ {
		task.recordHeartbeatFixture(heartbeatFixture{
			FiredAt:     int64(i + 1),
			OutcomeText: "NO_REPLY",
		})
	}
	entries, err := jsonlstore.Load[heartbeatFixture](task.heartbeatFixturePath())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != heartbeatFixtureCap {
		t.Fatalf("prune should keep exactly %d, got %d", heartbeatFixtureCap, len(entries))
	}
	if entries[0].FiredAt != int64(total-heartbeatFixtureCap+1) || entries[len(entries)-1].FiredAt != int64(total) {
		t.Fatalf("prune should keep the newest window in order: first=%d last=%d", entries[0].FiredAt, entries[len(entries)-1].FiredAt)
	}
}
