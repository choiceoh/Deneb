package schedule

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// ResolveReadWindow validates an optional explicit RFC3339 range or applies the
// calendar tool's bounded hours-ahead defaults.
func ResolveReadWindow(from, to string, hoursAhead int) (time.Time, time.Time, string) {
	return calResolveWindow(from, to, hoursAhead)
}

// MergeEvents reads Google and local calendars into one start-sorted range.
// The warning is non-fatal when one source failed and the other still answered.
func MergeEvents(ctx context.Context, deps *toolctx.CalendarDeps, from, to time.Time) ([]calendar.Event, string) {
	return calMerged(ctx, deps, from, to)
}
