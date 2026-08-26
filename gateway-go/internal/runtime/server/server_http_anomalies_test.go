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
