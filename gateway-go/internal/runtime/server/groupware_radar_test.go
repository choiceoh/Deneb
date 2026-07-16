package server

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

func TestFormatApprovalAnalysisFeed(t *testing.T) {
	doc := groupware.ApprovalSummary{
		DocID: "99178", Title: "구매 품의", DocNo: "EAP-42", Drafter: "홍길동",
		Date: "2026-07-16", Status: "결재대기",
	}
	rec := &groupware.ApprovalAnalysisRecord{
		DocID: "99178", Title: "구매 품의", Drafter: "홍길동", Date: "2026-07-16",
		Analysis: "요지: 구매\nIMPORTANCE: attention\n핵심: 단가 확인", Importance: "attention",
	}
	got := formatApprovalAnalysisFeed(doc, rec)
	for _, want := range []string{
		"## 구매 품의", "기안 홍길동", "2026-07-16", "EAP-42", "중요도 attention",
		"요지: 구매", "핵심: 단가 확인",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("feed missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "IMPORTANCE:") {
		t.Fatalf("IMPORTANCE marker should be stripped:\n%s", got)
	}
}

func TestGroupwareRadarCallbacksDedupeAndResolveByRefID(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	feed := &nativeWorkFeedStore{store: store}
	publishCalls := 0
	onPending, _, onResolved := groupwareRadarCallbacks(feed, func(_ context.Context, doc groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) error {
		publishCalls++
		if doc.DocID != "7" || rec == nil || rec.Analysis != "analysis" {
			t.Fatalf("publish doc=%q rec=%v", doc.DocID, rec)
		}
		_, err := store.Append(workfeed.Item{
			ID: "approval-card", Source: workfeed.SourceGroupwareApproval, RefID: "7", Body: rec.Analysis,
		})
		return err
	}, func(context.Context, groupware.ApprovalSummary, int, time.Duration) error { return nil },
		func(_ context.Context, doc groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
			return &groupware.ApprovalAnalysisRecord{DocID: doc.DocID, Analysis: "analysis"}, nil
		})
	doc := groupware.ApprovalSummary{DocID: "7", Title: "품의", Status: "미결"}
	if err := onPending(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if err := onPending(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if publishCalls != 1 {
		t.Fatalf("publish calls = %d, want RefID no-op", publishCalls)
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

func TestGroupwareRadarCallbacksAnalyzeBeforeFeed(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	feed := &nativeWorkFeedStore{store: store}
	var order []string
	onPending, _, _ := groupwareRadarCallbacks(feed, func(_ context.Context, doc groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) error {
		order = append(order, "publish")
		if rec == nil || rec.Analysis != "분석본문" {
			t.Fatalf("publish missing analysis: %#v", rec)
		}
		_, err := store.Append(workfeed.Item{
			ID: "approval-card", Source: workfeed.SourceGroupwareApproval, RefID: doc.DocID, Body: rec.Analysis,
		})
		return err
	}, nil, func(context.Context, groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
		order = append(order, "analyze")
		return &groupware.ApprovalAnalysisRecord{DocID: "9", Analysis: "분석본문"}, nil
	})
	if err := onPending(context.Background(), groupware.ApprovalSummary{DocID: "9", Title: "품의"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "analyze,publish" {
		t.Fatalf("order = %q, want analyze then publish", got)
	}
}

func TestGroupwareRadarCallbacksBlocksFeedWhenAnalyzeFails(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	feed := &nativeWorkFeedStore{store: store}
	publishCalls := 0
	onPending, _, _ := groupwareRadarCallbacks(feed, func(context.Context, groupware.ApprovalSummary, *groupware.ApprovalAnalysisRecord) error {
		publishCalls++
		return nil
	}, nil, func(context.Context, groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
		return nil, errors.New("analysis unavailable")
	})
	err := onPending(context.Background(), groupware.ApprovalSummary{DocID: "11", Title: "품의"})
	if err == nil || !strings.Contains(err.Error(), "analysis unavailable") {
		t.Fatalf("err = %v", err)
	}
	if publishCalls != 0 {
		t.Fatalf("publish must not run when analyze fails; calls=%d", publishCalls)
	}
	active, aerr := feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "11")
	if aerr != nil || active {
		t.Fatalf("no card expected; active=%v err=%v", active, aerr)
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
