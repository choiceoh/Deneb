package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

type stubArchivedMailAnalyzer struct{}

func (stubArchivedMailAnalyzer) AnalyzeArchived(context.Context, []*gmail.MessageDetail) ([]string, error) {
	return nil, nil
}

func TestRegisterMailBackfillTaskUsesIntakeAgnosticAnalyzer(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", filepath.Join(homeDir, config.DefaultStateDirname))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mailStore, err := mailstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mailStore.Close() })

	s := &Server{
		AutonomousSubsystem: &AutonomousSubsystem{
			autonomousSvc:        autonomous.NewService(logger),
			mailBackfillAnalyzer: stubArchivedMailAnalyzer{},
		},
		mailStore: mailStore,
		logger:    logger,
	}
	s.registerMailBackfillTask(homeDir)

	if got := s.autonomousSvc.TaskStatus("mail-backfill"); got == nil {
		t.Fatal("mail backfill task was not registered for a non-Gmail analyzer")
	}
}
