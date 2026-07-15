package schedule

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// ResolveReadWindow validates an optional explicit RFC3339 range or applies the
// calendar tool's bounded hours-ahead defaults.
func ResolveReadWindow(from, to string, hoursAhead int) (time.Time, time.Time, string) {
	return calResolveWindow(from, to, hoursAhead)
}

// MergeEvents reads Google and local calendars into one start-sorted range.
// The warning is non-fatal when one source failed and the other still answered.
func MergeEvents(ctx context.Context, deps *tooldeps.CalendarDeps, from, to time.Time) ([]tooldeps.CalendarEvent, string) {
	return calMerged(ctx, deps, from, to)
}
