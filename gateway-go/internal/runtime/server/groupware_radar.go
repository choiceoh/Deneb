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

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
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
	task := groupware.NewRadar(groupware.RadarConfig{
		Reader:         reader,
		StatePath:      filepath.Join(stateDir, groupwareRadarStateFile),
		MaxPerCycle:    groupwareRadarMaxPerCycle(),
		MaxEscalations: groupwareRadarMaxEscalations(),
		OnPending:      onPending,
		OnEscalated:    onEscalated,
		OnResolved:     onResolved,
	})
	s.autonomousSvc.RegisterTask(task)
	s.groupwareRadarActive = true
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

// publishApprovalAnalysisFeed posts the prepared analysis as the work-feed card
// (승인/반려 chips). Skips the phone-event judgment turn.
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
	content := formatApprovalAnalysisFeed(doc, rec)
	delivered, err := s.proactiveRelay.RelayNativeToOptions("", content, proactive.Options{
		WorkFeedSource: workfeed.SourceGroupwareApproval,
		RefID:          docID,
		ForceQuestion:  true,
		Actions:        groupwareApprovalActions(),
	})
	if err != nil {
		return err
	}
	if !delivered {
		return fmt.Errorf("groupware approval %s feed relay did not deliver", docID)
	}
	return nil
}

func formatApprovalAnalysisFeed(doc groupware.ApprovalSummary, rec *groupware.ApprovalAnalysisRecord) string {
	title := strings.TrimSpace(doc.Title)
	if title == "" && rec != nil {
		title = strings.TrimSpace(rec.Title)
	}
	if title == "" {
		title = "전자결재"
	}
	drafter := strings.TrimSpace(doc.Drafter)
	if drafter == "" && rec != nil {
		drafter = strings.TrimSpace(rec.Drafter)
	}
	date := strings.TrimSpace(doc.Date)
	if date == "" && rec != nil {
		date = strings.TrimSpace(rec.Date)
	}
	var meta []string
	if drafter != "" {
		meta = append(meta, "기안 "+drafter)
	}
	if date != "" {
		meta = append(meta, date)
	}
	if no := strings.TrimSpace(doc.DocNo); no != "" {
		meta = append(meta, no)
	}
	if rec != nil {
		if imp := strings.TrimSpace(rec.Importance); imp != "" {
			meta = append(meta, "중요도 "+imp)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", title)
	if len(meta) > 0 {
		b.WriteString(strings.Join(meta, " · "))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	if rec != nil {
		b.WriteString(stripApprovalImportanceMarker(rec.Analysis))
	}
	return strings.TrimSpace(b.String())
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
