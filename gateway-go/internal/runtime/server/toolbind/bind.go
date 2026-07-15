// Package toolbind holds concrete chat-tool bindings for the server composition
// root so internal/runtime/server does not import every tools/* leaf package.
package toolbind

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/linkenrichment"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/schedule"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind/observebind"
)

// ToolObserve wires the concrete observe tool (leaf imports live in observebind).
var ToolObserve = observebind.ToolObserve

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
