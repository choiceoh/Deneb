package routine

import (
	"context"
	"strings"
	"testing"
)

func TestFetchGroupwarePendingDisabledWithoutCredentials(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "")
	got, ok := fetchGroupwarePending(context.Background()).(groupwarePendingData)
	if !ok || got.Configured || got.OK || got.Count != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLetterDiarySummariesIncludePendingApprovals(t *testing.T) {
	gw := groupwarePendingData{OK: true, Configured: true, Count: 3}
	morning := make([]any, 8)
	morning[7] = gw
	if got := formatMorningDiarySummary("오늘", morning); !strings.Contains(got, "미결 전자결재: 3건") {
		t.Fatalf("morning %q", got)
	}
	evening := make([]any, 4)
	evening[3] = gw
	if got := formatEveningDiarySummary("오늘", evening); !strings.Contains(got, "미결 전자결재: 3건") {
		t.Fatalf("evening %q", got)
	}
}

func TestGroupwarePendingDataJSONContract(t *testing.T) {
	got := groupwarePendingData{OK: true, Configured: true, Count: 1, Items: []groupwarePendingEntry{{DocID: "99178", Title: "품의", Drafter: "김승리"}}}
	if !got.OK || !got.Configured || got.Items[0].DocID != "99178" {
		t.Fatalf("got %+v", got)
	}
}

func TestFetchGroupwareCCDisabledWithoutCredentials(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "")
	got, ok := fetchGroupwareCC(context.Background()).(groupwareCCData)
	if !ok || got.Configured || got.OK {
		t.Fatalf("got %+v", got)
	}
}

func TestFetchGroupwareCCWithoutRadarStateStaysEmptyAndOffline(t *testing.T) {
	// No radar state → the collector must return an empty OK section without
	// shelling out to the reader (credentials set, but nothing to enrich).
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_GROUPWARE_USER", "user")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "pass")
	got, ok := fetchGroupwareCC(context.Background()).(groupwareCCData)
	if !ok || !got.Configured || !got.OK || got.Count != 0 || got.Error != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestMorningDiarySummaryIncludesCCHighlights(t *testing.T) {
	morning := make([]any, 9)
	morning[8] = groupwareCCData{OK: true, Configured: true, Count: 2}
	if got := formatMorningDiarySummary("오늘", morning); !strings.Contains(got, "수신참조 신규: 2건") {
		t.Fatalf("morning %q", got)
	}
}

func TestLetterDiarySummariesIncludeStaleApprovals(t *testing.T) {
	gw := groupwarePendingData{OK: true, Configured: true, Count: 3, StaleCount: 1}
	morning := make([]any, 8)
	morning[7] = gw
	if got := formatMorningDiarySummary("오늘", morning); !strings.Contains(got, "방치 1건") {
		t.Fatalf("morning %q", got)
	}
	evening := make([]any, 4)
	evening[3] = gw
	if got := formatEveningDiarySummary("오늘", evening); !strings.Contains(got, "방치 1건") {
		t.Fatalf("evening %q", got)
	}
}
