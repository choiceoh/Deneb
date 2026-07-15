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
