package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

const groupwareRadarStateFile = "groupware_radar_state.json"

func (s *Server) registerGroupwareRadarTask(homeDir string) {
	if os.Getenv("DENEB_GROUPWARE_RADAR_DISABLE") == "1" || s.autonomousSvc == nil {
		return
	}
	reader, configured := groupware.FromEnv()
	if !configured {
		return
	}
	stateDir, production := s.productionStateDir(homeDir)
	if !production && os.Getenv("DENEB_GROUPWARE_RADAR_ALLOW_DEV") != "1" {
		return
	}
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		s.logger.Error("groupware radar disabled: work-feed store unavailable")
		return
	}

	onPending, onEscalated, onResolved := groupwareRadarCallbacks(
		feed,
		s.publishApprovalAnalysisFeed,
		s.notifyGroupwareRadarEscalation,
		s.prepareApprovalBeforeFeed,
	)
	var onCCNew func(context.Context, groupware.ApprovalSummary) error
	if os.Getenv("DENEB_GROUPWARE_RADAR_CC_DISABLE") != "1" {
		onCCNew = groupwareRadarCCCallback(s.prepareApprovalBeforeFeed, s.publishApprovalCCFeed)
	}
	// Native-sync mirror: list drift the radar just observed (new pending,
	// resolution, new 수신참조) drops the clients' approvals snapshot so the 결재
	// surface refetches on next view. Escalation is NOT drift — the row set is
	// unchanged — so it stays unwrapped. Warn on failure: TTL backstops.
	notifySync := func(docID string) {
		if s.nativeSyncStore == nil {
			return
		}
		if _, err := s.nativeSyncStore.Append(nativesync.ApprovalsChanged(docID)); err != nil {
			s.logger.Warn("native sync: approvals change append failed", "docId", docID, "error", err)
		}
	}
	wrapNotify := func(fn func(context.Context, groupware.ApprovalSummary) error) func(context.Context, groupware.ApprovalSummary) error {
		if fn == nil {
			return nil
		}
		return func(ctx context.Context, doc groupware.ApprovalSummary) error {
			if err := fn(ctx, doc); err != nil {
				return err
			}
			notifySync(doc.DocID)
			return nil
		}
	}
	onPending = wrapNotify(onPending)
	onResolved = wrapNotify(onResolved)
	onCCNew = wrapNotify(onCCNew)
	task := groupware.NewRadar(groupware.RadarConfig{
		Reader:         reader,
		StatePath:      filepath.Join(stateDir, groupwareRadarStateFile),
		MaxPerCycle:    groupwareRadarMaxPerCycle(),
		MaxEscalations: groupwareRadarMaxEscalations(),
		CCMaxPerCycle:  groupwareRadarCCMaxPerCycle(),
		OnPending:      onPending,
		OnEscalated:    onEscalated,
		OnResolved:     onResolved,
		OnCCNew:        onCCNew,
		OnListFailed:   s.notifyGroupwareRadarListFailed,
	})
	s.autonomousSvc.RegisterTask(task)
	s.groupwareRadar = task
	s.logger.Info("groupware radar task registered",
		"interval", task.Interval(),
		"maxPerCycle", groupwareRadarMaxPerCycle(),
		"maxEscalations", groupwareRadarMaxEscalations(),
		"stateDir", stateDir)
}

type (
	groupwareRadarPublishFeed   func(context.Context, groupware.ApprovalSummary, *groupware.ApprovalAnalysisRecord) error
	groupwareRadarEscalate      func(context.Context, groupware.ApprovalSummary, int, time.Duration) error
	groupwareRadarBeforePending func(context.Context, groupware.ApprovalSummary) (*groupware.ApprovalAnalysisRecord, error)
)

func groupwareRadarCallbacks(
	feed *nativeWorkFeedStore,
	publish groupwareRadarPublishFeed,
	escalate groupwareRadarEscalate,
	beforePending groupwareRadarBeforePending,
) (
	func(context.Context, groupware.ApprovalSummary) error,
	func(context.Context, groupware.ApprovalSummary, int, time.Duration) error,
	func(context.Context, groupware.ApprovalSummary) error,
) {
	onPending := func(ctx context.Context, doc groupware.ApprovalSummary) error {
		active, err := feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, doc.DocID)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
		// 조회→분석→(유의미하면)위키 → 분석 본문으로 피드. No second LLM turn.
		var rec *groupware.ApprovalAnalysisRecord
		if beforePending != nil {
			rec, err = beforePending(ctx, doc)
			if err != nil {
				return err
			}
		}
		if publish == nil {
			return errors.New("groupware radar approval publisher unavailable")
		}
		if err := publish(ctx, doc, rec); err != nil {
			return err
		}
		active, err = feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, doc.DocID)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("groupware approval %s relay completed without active card", doc.DocID)
		}
		return nil
	}
	onEscalated := func(ctx context.Context, doc groupware.ApprovalSummary, level int, age time.Duration) error {
		if escalate == nil {
			return errors.New("groupware radar escalation notifier unavailable")
		}
		return escalate(ctx, doc, level, age)
	}
	onResolved := func(_ context.Context, doc groupware.ApprovalSummary) error {
		return feed.AckBySourceRef(workfeed.SourceGroupwareApproval, doc.DocID)
	}
	return onPending, onEscalated, onResolved
}

// groupwareRadarCCCallback is the 수신참조 ingest: analyze (which files wiki
// prose and deal-ledger costs as side effects), then hand the record to the
// cc publisher. Analysis/read failures return an error so the radar keeps the
// doc retryable.
func groupwareRadarCCCallback(
	prepare groupwareRadarBeforePending,
	publish groupwareRadarPublishFeed,
) func(context.Context, groupware.ApprovalSummary) error {
	return func(ctx context.Context, doc groupware.ApprovalSummary) error {
		if prepare == nil || publish == nil {
			return errors.New("groupware radar cc pipeline unavailable")
		}
		rec, err := prepare(ctx, doc)
		if err != nil {
			return err
		}
		return publish(ctx, doc, rec)
	}
}

// publishApprovalCCFeed surfaces a 수신참조 analysis. cc documents belong to
// someone else's approval line, so the knowledge filing (project wiki + deal
// ledger) that happened during analysis IS the deliverable: only urgent ones
// additionally get a feed card — chip-less, question-less, push-less — and
// nothing escalates.
func (s *Server) publishApprovalCCFeed(_ context.Context, doc groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) error {
	if rec == nil || strings.TrimSpace(rec.Analysis) == "" {
		return fmt.Errorf("groupware cc %s analysis missing", strings.TrimSpace(doc.DocID))
	}
	if strings.TrimSpace(strings.ToLower(rec.Importance)) != "urgent" {
		return nil // silent ingest — wiki/ledger already filed
	}
	docID := strings.TrimSpace(doc.DocID)
	if docID == "" {
		docID = strings.TrimSpace(rec.DocID)
	}
	if docID == "" {
		return fmt.Errorf("groupware cc feed missing docId")
	}
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return errors.New("work-feed store unavailable")
	}
	active, err := feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, docID)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = strings.TrimSpace(rec.Title)
	}
	if title == "" {
		title = "전자결재"
	}
	analysisBody := stripApprovalImportanceMarker(rec.Analysis)
	item := workfeed.Item{
		Source:   workfeed.SourceGroupwareApproval,
		Title:    "수신참조 · " + title,
		Summary:  approvalAnalysisGlance(analysisBody),
		Body:     analysisBody,
		RefID:    docID,
		Metadata: map[string]string{"importance": rec.Importance, "folder": "cc"},
		Priority: workfeed.PriorityHigh,
	}
	_, err = feed.Append(item)
	return err
}

// publishApprovalAnalysisFeed posts the prepared analysis as the work-feed card
// (승인/반려 chips). Title/Summary/Body carry the analysis explicitly — no second
// LLM turn and no heuristic re-titling of the report blob.
func (s *Server) publishApprovalAnalysisFeed(_ context.Context, doc groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) error {
	if rec == nil || strings.TrimSpace(rec.Analysis) == "" {
		return fmt.Errorf("groupware approval %s analysis missing for feed", strings.TrimSpace(doc.DocID))
	}
	docID := strings.TrimSpace(doc.DocID)
	if docID == "" {
		docID = strings.TrimSpace(rec.DocID)
	}
	if docID == "" {
		return fmt.Errorf("groupware approval feed missing docId")
	}
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return errors.New("work-feed store unavailable")
	}
	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = strings.TrimSpace(rec.Title)
	}
	if title == "" {
		title = "전자결재"
	}
	analysisBody := stripApprovalImportanceMarker(rec.Analysis)
	summary := approvalAnalysisGlance(analysisBody)
	meta := map[string]string{}
	if imp := strings.TrimSpace(rec.Importance); imp != "" {
		meta["importance"] = imp
	}
	item := workfeed.Item{
		Source:   workfeed.SourceGroupwareApproval,
		Title:    title,
		Summary:  summary,
		Body:     analysisBody,
		RefID:    docID,
		Question: true,
		Actions:  groupwareApprovalActions(),
		Metadata: meta,
		Priority: approvalImportancePriority(rec.Importance),
	}
	out, err := feed.Append(item)
	if err != nil {
		return err
	}
	preview := strings.TrimSpace(out.Summary)
	if preview == "" {
		preview = out.Title
	}
	nativepush.PublishWithFallback(s.pushHub, s.pushNotifier, nativepush.Event{
		Title: "Deneb",
		Body:  preview,
		Kind:  nativepush.PushKindWorkfeed,
		Ref:   out.ID,
	})
	return nil
}

// approvalAnalysisGlance pulls the 요지 line for the collapsed feed row.
func approvalAnalysisGlance(analysis string) string {
	if gist := groupware.ApprovalAnalysisGistLine(analysis); gist != "" {
		return workfeed.Preview(gist, 240)
	}
	return workfeed.Preview(analysis, 240)
}

func approvalImportancePriority(importance string) int {
	switch strings.TrimSpace(strings.ToLower(importance)) {
	case "urgent":
		return workfeed.PriorityUrgent
	case "attention":
		return workfeed.PriorityHigh
	case "routine":
		return workfeed.PriorityLow
	default:
		return 0
	}
}

func (s *Server) notifyGroupwareRadarEscalation(_ context.Context, doc groupware.ApprovalSummary, level int, age time.Duration) error {
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return errors.New("work-feed store unavailable")
	}
	label := groupwareEscalationLabel(level, age)
	updated, err := feed.EscalateApprovalBySourceRef(doc.DocID, level, label)
	if err != nil || !updated {
		return err
	}
	content := fmt.Sprintf("전자결재 방치 알림\n\n**%s** · %s 미결입니다. 확인이 필요합니다.", strings.TrimSpace(doc.Title), label)
	_, err = s.proactiveRelay.RelayNativeToOptions("", content, proactive.Options{WorkFeedSource: workfeed.SourceGroupwareApproval, RefID: doc.DocID, ForceQuestion: true, Actions: groupwareApprovalActions()})
	return err
}

// notifyGroupwareRadarListFailed posts one ops card after repeated Amaranth list
// failures so the outage is visible in the feed, not only journald.
func (s *Server) notifyGroupwareRadarListFailed(_ context.Context, folder string, streak int, listErr error) error {
	feed := s.nativeWorkFeedStore()
	if feed == nil {
		return errors.New("work-feed store unavailable")
	}
	refID := groupware.RadarListFailRefID
	active, err := feed.HasActiveSourceRef(workfeed.SourceProactive, refID)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	detail := ""
	if listErr != nil {
		detail = strings.TrimSpace(listErr.Error())
	}
	summary := fmt.Sprintf("미결 목록 %d회 연속 실패 (%s)", streak, strings.TrimSpace(folder))
	body := fmt.Sprintf(
		"전자결재 레이더가 Amaranth 목록을 읽지 못했습니다.\n\n- 폴더: %s\n- 연속 실패: %d회\n- 오류: %s\n\n다음 주기에 자동 재시도합니다. 로그인·세션·리더 스크립트를 확인하세요.",
		strings.TrimSpace(folder), streak, detail,
	)
	item := workfeed.Item{
		Source:   workfeed.SourceProactive,
		Title:    "전자결재 레이더 장애",
		Summary:  summary,
		Body:     body,
		RefID:    refID,
		Priority: workfeed.PriorityHigh,
	}
	out, err := feed.Append(item)
	if err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Error("groupware radar list failed — feed alert posted",
			"folder", folder, "streak", streak, "err", detail)
	}
	preview := strings.TrimSpace(out.Summary)
	if preview == "" {
		preview = out.Title
	}
	nativepush.PublishWithFallback(s.pushHub, s.pushNotifier, nativepush.Event{
		Title: "Deneb",
		Body:  preview,
		Kind:  nativepush.PushKindWorkfeed,
		Ref:   out.ID,
	})
	return nil
}

func groupwareEscalationLabel(level int, age time.Duration) string {
	if level >= groupware.RadarEscalationLevelTwentyFour {
		return "24시간 이상"
	}
	hours := int(age.Round(time.Hour) / time.Hour)
	if hours < 4 {
		hours = 4
	}
	return fmt.Sprintf("%d시간째", hours)
}

func groupwareRadarMaxPerCycle() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_RADAR_MAX_PER_CYCLE")))
	if err == nil && value > 0 {
		return value
	}
	return groupware.DefaultRadarMaxPerCycle
}

func groupwareRadarMaxEscalations() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_RADAR_MAX_ESCALATIONS")))
	if err == nil && value > 0 {
		return value
	}
	return groupware.DefaultRadarMaxEscalations
}

func groupwareRadarCCMaxPerCycle() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_RADAR_CC_MAX_PER_CYCLE")))
	if err == nil && value > 0 {
		return value
	}
	return groupware.DefaultRadarCCMaxPerCycle
}
