package heartbeat

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAutoApplyFixtures persists enough usable heartbeat fixtures for the
// shadow gate to pass judgment (>= heartbeatShadowMinFixtures).
func writeAutoApplyFixtures(t *testing.T, path string, n int) {
	t.Helper()
	var lines []string
	for i := 0; i < n; i++ {
		f := heartbeatFixture{
			FiredAt:     time.Now().Add(-time.Duration(n-i) * time.Hour).UnixMilli(),
			HeartbeatMD: "## Active Tasks\n- 없음",
			OutcomeText: "NO_REPLY",
		}
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Flag off: even an accept-verdict candidate stays propose-only and the live
// contract is untouched.
func TestMaybeAutoApplyCandidate_FlagOffIsProposeOnly(t *testing.T) {
	dir := t.TempDir()
	hb := filepath.Join(dir, "HEARTBEAT.md")
	fixtures := filepath.Join(dir, "fixtures.jsonl")
	if err := os.WriteFile(hb, []byte("원본 계약"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAutoApplyFixtures(t, fixtures, 6)
	complete := func(_ context.Context, _, _ string) (string, error) { return "NO_REPLY", nil }

	applied, verdict, err := MaybeAutoApplyCandidate(context.Background(), hb, fixtures, strings.Repeat("후보 계약. ", 20), complete, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("flag off must never apply")
	}
	if !strings.Contains(verdict, "flag off") {
		t.Fatalf("verdict must say the flag gated it: %q", verdict)
	}
	if got, _ := os.ReadFile(hb); string(got) != "원본 계약" {
		t.Fatalf("live contract mutated with flag off: %q", got)
	}
}

// Flag on + accept verdict: the candidate lands with a backup + armed marker;
// the anomaly watch then restores the backup after K consecutive failures.
func TestMaybeAutoApplyCandidate_AppliesAndRollsBack(t *testing.T) {
	t.Setenv(heartbeatAutoApplyEnv, "1")
	dir := t.TempDir()
	hb := filepath.Join(dir, "HEARTBEAT.md")
	fixtures := filepath.Join(dir, "fixtures.jsonl")
	if err := os.WriteFile(hb, []byte("원본 계약"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAutoApplyFixtures(t, fixtures, 6)
	// The original contract chatters on quiet fixtures (fails the NO_REPLY
	// verifier); the candidate stays quiet — a measurable improvement, so the
	// no-trade-off gate accepts.
	complete := func(_ context.Context, _, user string) (string, error) {
		if strings.Contains(user, "개정 계약") {
			return "NO_REPLY", nil
		}
		return "알릴 것 없음에도 불필요한 보고를 남깁니다.", nil
	}

	candidate := strings.Repeat("개정 계약. ", 20)
	applied, verdict, err := MaybeAutoApplyCandidate(context.Background(), hb, fixtures, candidate, complete, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatalf("accept verdict with flag on must apply (verdict=%q)", verdict)
	}
	if got, _ := os.ReadFile(hb); !strings.Contains(string(got), "개정 계약") {
		t.Fatalf("candidate not written: %q", got)
	}
	if got, _ := os.ReadFile(hb + ".autoapply.bak"); string(got) != "원본 계약" {
		t.Fatalf("backup missing pre-apply contract: %q", got)
	}

	// One success resets the streak; K consecutive failures restore.
	noteHeartbeatTurnOutcome(hb, false, slog.Default())
	noteHeartbeatTurnOutcome(hb, true, slog.Default())
	for i := 0; i < heartbeatAutoApplyRollbackThreshold; i++ {
		noteHeartbeatTurnOutcome(hb, false, slog.Default())
	}
	if got, _ := os.ReadFile(hb); string(got) != "원본 계약" {
		t.Fatalf("anomaly watch did not restore the backup: %q", got)
	}
	if _, err := os.Stat(hb + ".autoapply.json"); !os.IsNotExist(err) {
		t.Fatal("marker must be disarmed after restore")
	}
	// Disarmed: further failures must not thrash the restored contract.
	noteHeartbeatTurnOutcome(hb, false, slog.Default())
	if got, _ := os.ReadFile(hb); string(got) != "원본 계약" {
		t.Fatalf("disarmed watch mutated the contract: %q", got)
	}
}
