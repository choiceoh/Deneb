package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

func TestFormatGroupwareRadarNotification(t *testing.T) {
	doc := groupware.ApprovalSummary{
		DocID: "99178", Title: "구매 품의", DocNo: "EAP-42", Drafter: "홍길동",
		Date: "2026-07-16", Status: "결재대기", Folder: "pending",
	}
	got := formatGroupwareRadarNotification(doc)
	for _, want := range []string{
		"종류: 전자결재", "상태: 결재대기", "제목: 구매 품의", "문서ID: 99178",
		"문서번호: EAP-42", "기안: 홍길동", "기안일: 2026-07-16",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("notification missing %q:\n%s", want, got)
		}
	}
}

func TestGroupwareRadarCallbacksDedupeAndResolveByRefID(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	feed := &nativeWorkFeedStore{store: store}
	ingestCalls := 0
	onPending, _, onResolved := groupwareRadarCallbacks(feed, func(_ context.Context, source, text string) error {
		ingestCalls++
		if source != "groupware-radar" || !strings.Contains(text, "문서ID: 7") {
			t.Fatalf("ingest source=%q text=%q", source, text)
		}
		_, err := store.Append(workfeed.Item{
			ID: "approval-card", Source: workfeed.SourceGroupwareApproval, RefID: "7", Body: "analysis",
		})
		return err
	}, func(context.Context, groupware.ApprovalSummary, int, time.Duration) error { return nil })
	doc := groupware.ApprovalSummary{DocID: "7", Title: "품의", Status: "미결"}
	if err := onPending(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if err := onPending(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if ingestCalls != 1 {
		t.Fatalf("ingest calls = %d, want phone+poll RefID no-op", ingestCalls)
	}
	if err := onResolved(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	active, err := feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "7")
	if err != nil || active {
		t.Fatalf("resolved active=%v err=%v", active, err)
	}
	if err := onResolved(context.Background(), doc); err != nil {
		t.Fatalf("idempotent resolve: %v", err)
	}
}

func TestGroupwareRadarMaxPerCyclePositiveOverride(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_RADAR_MAX_PER_CYCLE", "5")
	if got := groupwareRadarMaxPerCycle(); got != 5 {
		t.Fatalf("max = %d, want 5", got)
	}
	t.Setenv("DENEB_GROUPWARE_RADAR_MAX_PER_CYCLE", "0")
	if got := groupwareRadarMaxPerCycle(); got != groupware.DefaultRadarMaxPerCycle {
		t.Fatalf("invalid max = %d, want default", got)
	}
}

func TestGroupwareEscalationLabel(t *testing.T) {
	if got := groupwareEscalationLabel(groupware.RadarEscalationLevelFourHours, 5*time.Hour); got != "5시간째" {
		t.Fatalf("got %q", got)
	}
	if got := groupwareEscalationLabel(groupware.RadarEscalationLevelTwentyFour, 26*time.Hour); got != "24시간 이상" {
		t.Fatalf("got %q", got)
	}
}

func TestGroupwareRadarMaxEscalationsOverride(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_RADAR_MAX_ESCALATIONS", "4")
	if got := groupwareRadarMaxEscalations(); got != 4 {
		t.Fatalf("got %d", got)
	}
	t.Setenv("DENEB_GROUPWARE_RADAR_MAX_ESCALATIONS", "0")
	if got := groupwareRadarMaxEscalations(); got != groupware.DefaultRadarMaxEscalations {
		t.Fatalf("default %d", got)
	}
}
