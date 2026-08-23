package genesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A healthy bench with planned=0 read as a clean "v3 · 방금" row while every
// candidate was reopen-blocked for weeks (2026-08-23: 13/13). Blocked supply
// must read as blocked; the deadcode lane must show its last run at all.
func TestRSIStatus_L4SurfacesBlockedSupplyAndDeadcodeLane(t *testing.T) {
	tr := newTestTracker(t)
	dir := filepath.Dir(tr.selfCorrectionPath)

	if err := os.WriteFile(filepath.Join(dir, "health_finding_miner_status.json"),
		[]byte(`{"lastRunAtMs":1,"structuralSource":"health-bench-v3","fallbackReason":"","planned":0,"filed":0,"skipped":13,"blockedPermanent":8,"blockedCooldown":5,"capped":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	got := rsiMetricValue(l.Metrics, "공급 벤치")
	if !strings.HasPrefix(got, "v3 · ") || !strings.Contains(got, "공급 차단 13건") || !strings.Contains(got, "영구 8") {
		t.Fatalf("blocked supply not surfaced: %q", got)
	}
	if got := rsiMetricValue(l.Metrics, "deadcode 마이너"); got != "기록 없음" {
		t.Fatalf("deadcode lane without status = %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "health_finding_miner_status.json"),
		[]byte(`{"lastRunAtMs":1,"structuralSource":"health-bench-v3","planned":2,"filed":2,"skipped":4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deadcode_finding_miner_status.json"),
		[]byte(`{"lastRunAtMs":1,"ok":false,"error":"deadcode-audit tooling failure (rc=2): failed to run deadcode","findings":0,"planned":0,"filed":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l = rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if got := rsiMetricValue(l.Metrics, "공급 벤치"); !strings.Contains(got, "후보 2 / 제출 2") {
		t.Fatalf("supplied lane = %q", got)
	}
	if got := rsiMetricValue(l.Metrics, "deadcode 마이너"); !strings.HasPrefix(got, "⚠ 실패") || !strings.Contains(got, "rc=2") {
		t.Fatalf("failed deadcode run = %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "deadcode_finding_miner_status.json"),
		[]byte(`{"lastRunAtMs":1,"ok":true,"error":"","findings":3,"planned":2,"filed":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l = rsiLayerByKey(tr.RSIStatus().Layers, "L4")
	if got := rsiMetricValue(l.Metrics, "deadcode 마이너"); !strings.HasPrefix(got, "정상") || !strings.Contains(got, "신규 3 / 후보 2 / 제출 2") {
		t.Fatalf("healthy deadcode run = %q", got)
	}
}
