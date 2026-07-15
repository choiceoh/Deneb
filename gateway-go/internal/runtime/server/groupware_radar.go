package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
)

const groupwareRadarStateFile = "groupware_radar_state.json"

func (s *Server) registerGroupwareRadarTask(homeDir string) {
	if os.Getenv("DENEB_GROUPWARE_RADAR_DISABLE") == "1" || s.autonomousSvc == nil || s.chatHandler == nil {
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

	onPending, onResolved := groupwareRadarCallbacks(feed, func(ctx context.Context, source, text string) error {
		return phoneevents.New(s.phoneEventHandlerConfig()).IngestApprovalSync(ctx, source, text)
	})
	task := groupware.NewRadar(groupware.RadarConfig{
		Reader:      reader,
		StatePath:   filepath.Join(stateDir, groupwareRadarStateFile),
		MaxPerCycle: groupwareRadarMaxPerCycle(),
		OnPending:   onPending,
		OnResolved:  onResolved,
	})
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("groupware radar task registered",
		"interval", task.Interval(), "maxPerCycle", groupwareRadarMaxPerCycle(), "stateDir", stateDir)
}

type groupwareRadarIngest func(context.Context, string, string) error

func groupwareRadarCallbacks(
	feed *nativeWorkFeedStore,
	ingest groupwareRadarIngest,
) (
	func(context.Context, groupware.ApprovalSummary) error,
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
		if ingest == nil {
			return errors.New("groupware radar approval ingester unavailable")
		}
		if err := ingest(ctx, "groupware-radar", formatGroupwareRadarNotification(doc)); err != nil {
			return err
		}
		// Relay work-feed append is historically best-effort. Verify the durable
		// RefID postcondition so radar state is not marked notified after a lost card.
		active, err = feed.HasActiveSourceRef(workfeed.SourceGroupwareApproval, doc.DocID)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("groupware approval %s relay completed without active card", doc.DocID)
		}
		return nil
	}
	onResolved := func(_ context.Context, doc groupware.ApprovalSummary) error {
		return feed.AckBySourceRef(workfeed.SourceGroupwareApproval, doc.DocID)
	}
	return onPending, onResolved
}

func formatGroupwareRadarNotification(doc groupware.ApprovalSummary) string {
	return strings.Join([]string{
		"종류: 전자결재",
		"상태: " + strings.TrimSpace(doc.Status),
		"제목: " + strings.TrimSpace(doc.Title),
		"문서ID: " + strings.TrimSpace(doc.DocID),
		"문서번호: " + strings.TrimSpace(doc.DocNo),
		"기안: " + strings.TrimSpace(doc.Drafter),
		"기안일: " + strings.TrimSpace(doc.Date),
	}, "\n")
}

func groupwareRadarMaxPerCycle() int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("DENEB_GROUPWARE_RADAR_MAX_PER_CYCLE")))
	if err == nil && value > 0 {
		return value
	}
	return groupware.DefaultRadarMaxPerCycle
}
