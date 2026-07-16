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

func TestApprovalAnalysisGlanceAndPriority(t *testing.T) {
	analysis := "**요지** — 구매 단가 확인 필요\n**핵심**\n- 금액 3억\nIMPORTANCE: attention"
	body := stripApprovalImportanceMarker(analysis)
	if got := approvalAnalysisGlance(body); got != "구매 단가 확인 필요" {
		t.Fatalf("glance = %q", got)
	}
	if strings.Contains(body, "IMPORTANCE:") {
		t.Fatal("IMPORTANCE should be stripped from body")
	}
	if approvalImportancePriority("urgent") != workfeed.PriorityUrgent {
		t.Fatal("urgent priority")
	}
	if approvalImportancePriority("attention") != workfeed.PriorityHigh {
		t.Fatal("attention priority")
	}
	if approvalImportancePriority("routine") != workfeed.PriorityLow {
		t.Fatal("routine priority")
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

func TestPublishApprovalCCFeedUrgentOnlyChiplessCard(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	s := &Server{MemorySubsystem: &MemorySubsystem{workFeedStore: store}}
	doc := groupware.ApprovalSummary{DocID: "42", Title: "긴급 계약 변경", Folder: "cc"}

	// attention/routine stay silent — knowledge filing already happened.
	rec := &groupware.ApprovalAnalysisRecord{DocID: "42", Analysis: "**요지** — 일반 경비\nIMPORTANCE: attention", Importance: "attention"}
	if err := s.publishApprovalCCFeed(context.Background(), doc, rec); err != nil {
		t.Fatal(err)
	}
	if _, active, err := store.FindActiveBySourceRef(workfeed.SourceGroupwareApproval, "42"); err != nil || active {
		t.Fatalf("attention cc must not card; active=%v err=%v", active, err)
	}

	// urgent gets one informational card: no chips, no question.
	rec = &groupware.ApprovalAnalysisRecord{DocID: "42", Analysis: "**요지** — 긴급 계약 변경 통지\nIMPORTANCE: urgent", Importance: "urgent"}
	if err := s.publishApprovalCCFeed(context.Background(), doc, rec); err != nil {
		t.Fatal(err)
	}
	item, active, err := store.FindActiveBySourceRef(workfeed.SourceGroupwareApproval, "42")
	if err != nil || !active {
		t.Fatalf("urgent cc card missing; active=%v err=%v", active, err)
	}
	if item.Question {
		t.Fatalf("cc card must not ask: %+v", item)
	}
	// The store decorates generic actions (open/snooze/…) — what must never
	// appear on a cc card is the 승인/반려 pair (someone else's decision).
	for _, action := range item.Actions {
		if action.ID == groupwareApprovalActionApprove || action.ID == groupwareApprovalActionReject {
			t.Fatalf("cc card carries approval chip %q: %+v", action.ID, item.Actions)
		}
	}
	if !strings.HasPrefix(item.Title, "수신참조 · ") {
		t.Fatalf("cc card title = %q", item.Title)
	}
	if strings.Contains(item.Body, "IMPORTANCE:") {
		t.Fatalf("machine trailer leaked into body: %q", item.Body)
	}
	if item.Metadata["folder"] != "cc" {
		t.Fatalf("cc card metadata = %v", item.Metadata)
	}

	// Duplicate urgent publish is a no-op while the card is active.
	if err := s.publishApprovalCCFeed(context.Background(), doc, rec); err != nil {
		t.Fatal(err)
	}
	items, _, err := store.List(10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("duplicate publish appended a second card: %d", len(items))
	}

	// Missing analysis is an error so the radar keeps the doc retryable.
	if err := s.publishApprovalCCFeed(context.Background(), doc, nil); err == nil {
		t.Fatal("nil record must error")
	}
}

func TestGroupwareRadarCCCallbackAnalyzesThenPublishes(t *testing.T) {
	var order []string
	onCC := groupwareRadarCCCallback(
		func(_ context.Context, doc groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
			order = append(order, "analyze")
			return &groupware.ApprovalAnalysisRecord{DocID: doc.DocID, Analysis: "분석"}, nil
		},
		func(_ context.Context, _ groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) error {
			order = append(order, "publish")
			if rec == nil || rec.Analysis != "분석" {
				t.Fatalf("publish rec = %#v", rec)
			}
			return nil
		},
	)
	if err := onCC(context.Background(), groupware.ApprovalSummary{DocID: "9", Folder: "cc"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "analyze,publish" {
		t.Fatalf("order = %q", got)
	}

	failing := groupwareRadarCCCallback(
		func(context.Context, groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error) {
			return nil, errors.New("analysis unavailable")
		},
		func(context.Context, groupware.ApprovalSummary, *groupware.ApprovalAnalysisRecord) error {
			t.Fatal("publish must not run when analyze fails")
			return nil
		},
	)
	if err := failing(context.Background(), groupware.ApprovalSummary{DocID: "9"}); err == nil {
		t.Fatal("expected analyze failure to propagate")
	}
}

func TestGroupwareRadarCCMaxPerCycleOverride(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_RADAR_CC_MAX_PER_CYCLE", "4")
	if got := groupwareRadarCCMaxPerCycle(); got != 4 {
		t.Fatalf("got %d", got)
	}
	t.Setenv("DENEB_GROUPWARE_RADAR_CC_MAX_PER_CYCLE", "0")
	if got := groupwareRadarCCMaxPerCycle(); got != groupware.DefaultRadarCCMaxPerCycle {
		t.Fatalf("default %d", got)
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
