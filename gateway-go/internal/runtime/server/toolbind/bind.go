// Package toolbind holds concrete chat-tool bindings for the server composition
// root so internal/runtime/server does not import every tools/* leaf package.
package toolbind

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/linkenrichment"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/schedule"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/docmedia"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/observebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/weekly"
)

// ToolObserve wires the concrete observe tool (leaf imports live in observebind).
var ToolObserve = observebind.ToolObserve

// Doc/media leaf bindings (OCR, attachment text, ASR) — re-exported so server
// composition files import toolbind once instead of toolbind/docmedia.
var (
	OCRImage              = docmedia.OCRImage
	ExtractAttachmentText = docmedia.ExtractAttachmentText
	ExtractDocumentText   = docmedia.ExtractDocumentText
	TranscribeAudio       = docmedia.TranscribeAudio
	TranslateSegments     = docmedia.TranslateSegments
)

type LocalAIFunc = docmedia.LocalAIFunc

// Skill-lifecycle leaf bindings.
type (
	HeartbeatShadowReplayResult        = lifecycle.HeartbeatShadowReplayResult
	HeartbeatShadowReplayFixtureResult = lifecycle.HeartbeatShadowReplayFixtureResult
	SkillLifecycleBackend              = lifecycle.SkillLifecycleBackend
	ToolFunc                           = lifecycle.ToolFunc
)

var (
	SkillLifecycleToolDescription = lifecycle.SkillLifecycleToolDescription
	SkillLifecycleToolSchema      = lifecycle.SkillLifecycleToolSchema
	ToolSkillLifecycle            = lifecycle.ToolSkillLifecycle
)

// Phone ops leaf binding (hosted under lifecycle to keep this package ≤ soft fanout).
var ErrPhoneActionUnconfirmed = lifecycle.ErrPhoneActionUnconfirmed

// Recurring briefing leaf bindings.
type (
	MorningLetterOpts = weekly.MorningLetterOpts
	WeeklyReportOpts  = weekly.WeeklyReportOpts
)

func CollectMorningLetterData(ctx context.Context, opts MorningLetterOpts, now time.Time) (string, error) {
	return weekly.CollectMorningLetterData(ctx, opts, now)
}

func RenderMorningLetterCard(dataJSON, narrativeJSON string, now time.Time) (string, error) {
	return weekly.RenderMorningLetterCard(dataJSON, narrativeJSON, now)
}

func CollectWeeklyReportData(ctx context.Context, opts WeeklyReportOpts, now time.Time) (string, error) {
	return weekly.CollectWeeklyReportData(ctx, opts, now)
}

func RenderWeeklyReportCard(opts WeeklyReportOpts, now time.Time) string {
	return weekly.RenderWeeklyReportCard(opts, now)
}

func BuildWeeklyReportImage(ctx context.Context, opts WeeklyReportOpts, now time.Time) ([]byte, bool) {
	return weekly.BuildWeeklyReportImage(ctx, opts, now)
}

func NewCalendarGlance(d *tooldeps.CalendarDeps) chat.CalendarGlanceFunc {
	return chat.CalendarGlanceFunc(schedule.NewCalendarGlanceFunc(d))
}

// NewLinkEnrichStart wires the concrete linkenrichment engine for chat.HandlerConfig.
func NewLinkEnrichStart(logger *slog.Logger) chat.LinkEnrichStart {
	engine := linkenrichment.New(linkenrichment.Config{Logger: logger})
	return func(ctx context.Context, message string, sanitize func(string) string) func(context.Context) string {
		return engine.Start(ctx, message, sanitize)
	}
}
