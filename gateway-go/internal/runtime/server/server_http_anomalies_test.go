package server

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/anomalywatch"
)

// TestAnomalyDigestDistinguishesQuietRuntimeFromSilentWatcher is the reading
// this route exists to get right. An empty ledger is NOT good news, and a
// digest that renders it like a clean bill of health would recreate exactly the
// confusion the lane was built to remove.
func TestAnomalyDigestDistinguishesQuietRuntimeFromSilentWatcher(t *testing.T) {
	silent := renderAnomalyDigest(nil, 24*time.Hour)
	if !strings.Contains(silent, "감시 레인이 돌지 않았다") {
		t.Errorf("an empty ledger must read as a silent WATCHER, not a quiet runtime:\n%s", silent)
	}

	quiet := renderAnomalyDigest([]anomalywatch.Entry{
		{At: "2026-08-26T01:00:00Z", Examined: anomalywatch.Examined{LogLines: 9}},
	}, 24*time.Hour)
	if strings.Contains(quiet, "감시 레인이 돌지 않았다") {
		t.Errorf("a recorded clean pass must not read as a silent watcher:\n%s", quiet)
	}
	if !strings.Contains(quiet, "이상 관측 없음") {
		t.Errorf("a clean pass must say so:\n%s", quiet)
	}
}

// TestAnomalyDigestCarriesEvidenceInline: the quote is what makes dismissing a
// false positive cost seconds instead of an investigation, so it must survive
// into the rendered page.
func TestAnomalyDigestCarriesEvidenceInline(t *testing.T) {
	out := renderAnomalyDigest([]anomalywatch.Entry{{
		At:       "2026-08-26T02:00:00Z",
		Examined: anomalywatch.Examined{LogLines: 40},
		Findings: []anomalywatch.Finding{{
			Severity: "high", Summary: "메일 배달이 반복 실패",
			Evidence: "[ERROR]×12 mail: LMTP 배달 실패 | code=451", WhyItMatters: "수신 메일이 유실된다",
		}},
	}}, 24*time.Hour)
	for _, want := range []string{"메일 배달이 반복 실패", "LMTP 배달 실패", "code=451", "수신 메일이 유실된다", "HIGH"} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %q:\n%s", want, out)
		}
	}
}

// TestAnomalyDigestSurfacesGapsSeparately: a pass that could not reach a
// verdict is louder than a clean one and must never be counted as clean.
func TestAnomalyDigestSurfacesGapsSeparately(t *testing.T) {
	out := renderAnomalyDigest([]anomalywatch.Entry{
		{At: "2026-08-26T03:00:00Z", Gap: "판정 호출 실패: connection refused"},
		{At: "2026-08-26T02:00:00Z"},
	}, 24*time.Hour)
	if !strings.Contains(out, "판정불가 1회") {
		t.Errorf("gap must be counted apart from clean passes:\n%s", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Errorf("gap reason must be shown:\n%s", out)
	}
	if !strings.Contains(out, "무소견 1회") {
		t.Errorf("the clean pass must still be counted:\n%s", out)
	}
}

// TestAnomalyDigestFlagsTruncatedWindows: a stretch of clean passes over
// truncated windows is much weaker evidence than its count suggests, and the
// digest must say so rather than letting the count speak for itself.
func TestAnomalyDigestFlagsTruncatedWindows(t *testing.T) {
	out := renderAnomalyDigest([]anomalywatch.Entry{{
		At: "2026-08-26T08:21:10Z", WindowMinutes: 90,
		Examined: anomalywatch.Examined{LogLines: 1, CoveredMinutes: 6, Partial: true},
		Findings: []anomalywatch.Finding{{Severity: "low", Summary: "s", Evidence: "e"}},
	}}, 12*time.Hour)
	if !strings.Contains(out, "창이 잘렸다") {
		t.Errorf("truncated windows must be called out:\n%s", out)
	}
	if !strings.Contains(out, "90분 중 6분") {
		t.Errorf("per-pass coverage must be explicit:\n%s", out)
	}
}

// TestCollapseFindingsGroupsAcrossPassesDespiteRewording: an hourly lane
// re-reports a standing problem every hour and rewrites the summary each time,
// so grouping must key on the verbatim evidence, not the prose.
func TestCollapseFindingsGroupsAcrossPassesDespiteRewording(t *testing.T) {
	ev := "[WARN] mcp server sent unparseable line | bytes=18 error=invalid character 'C'"
	entries := []anomalywatch.Entry{
		{At: "2026-08-26T22:00:00Z", Findings: []anomalywatch.Finding{
			{Severity: "medium", Summary: "MCP 서버가 파싱 불가능한 라인을 보낸다", Evidence: ev},
		}},
		{At: "2026-08-26T08:00:00Z", Findings: []anomalywatch.Finding{
			{Severity: "low", Summary: "MCP 서버가 JSON이 아닌 줄을 보냈다", Evidence: ev},
		}},
		{At: "2026-08-26T12:00:00Z", Findings: []anomalywatch.Finding{
			{Severity: "low", Summary: "genesis 드레이너 실패", Evidence: "[WARN] genesis-backlog-drain: generate failed"},
		}},
	}
	got := collapseFindings(entries)
	if len(got) != 2 {
		t.Fatalf("collapsed to %d groups, want 2: %+v", len(got), got)
	}
	top := got[0]
	if top.count != 2 {
		t.Errorf("repeat count = %d, want 2 — differently-worded summaries of one line are one problem", top.count)
	}
	if top.severity != "medium" {
		t.Errorf("severity = %q, want the highest any pass assigned", top.severity)
	}
	if top.first != "2026-08-26T08:00:00Z" || top.last != "2026-08-26T22:00:00Z" {
		t.Errorf("standing window = %s ~ %s, want the full span", top.first, top.last)
	}
	if top.summary != "MCP 서버가 파싱 불가능한 라인을 보낸다" {
		t.Errorf("summary = %q, want the newest wording", top.summary)
	}
}

// TestEvidenceKeyFoldsVolatileNumbersButNotDistinctLines.
func TestEvidenceKeyFoldsVolatileNumbersButNotDistinctLines(t *testing.T) {
	if evidenceKey("mcp unparseable | bytes=18") != evidenceKey("mcp unparseable | bytes=24") {
		t.Error("volatile counts must not split one problem into many")
	}
	if evidenceKey("mcp unparseable line") == evidenceKey("genesis parse failed") {
		t.Error("genuinely different lines must not merge")
	}
}
