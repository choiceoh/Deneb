package chat

import "context"

// CalendarGlanceFunc returns the ambient upcoming-events glance for a turn.
// Empty string = no events / unavailable → caller injects no section. A nil
// CalendarGlanceFunc disables the feature entirely.
//
// Construction lives in tools/schedule.NewCalendarGlanceFunc so the chat parent
// package does not import the schedule tool subpackage.
type CalendarGlanceFunc func(ctx context.Context, sessionKey, tz string) string
