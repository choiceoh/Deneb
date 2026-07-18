package weekly

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
)

type (
	WeeklyReportOpts  = routine.WeeklyReportOpts
	MorningLetterOpts = routine.MorningLetterOpts
)

func CollectMorningLetterData(ctx context.Context, opts MorningLetterOpts, now time.Time) (string, error) {
	return routine.CollectMorningLetterData(ctx, opts, now)
}

func RenderMorningLetterCard(dataJSON, narrativeJSON string, now time.Time) (string, error) {
	return routine.RenderMorningLetterCard(dataJSON, narrativeJSON, now)
}

func CollectWeeklyReportData(ctx context.Context, opts WeeklyReportOpts, now time.Time) (string, error) {
	return routine.CollectWeeklyReportData(ctx, opts, now)
}

func RenderWeeklyReportCard(opts WeeklyReportOpts, now time.Time) string {
	return routine.RenderWeeklyReportCard(opts, now)
}

func BuildWeeklyReportImage(ctx context.Context, opts WeeklyReportOpts, now time.Time) ([]byte, bool) {
	return routine.BuildWeeklyReportImage(ctx, opts, now)
}
