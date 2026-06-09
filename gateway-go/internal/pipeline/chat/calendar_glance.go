// calendar_glance.go builds the ambient calendar glance for the dynamic
// system-prompt block.
//
// The glance is frozen per calendar day (one global slot — single-user
// deployment, so all sessions share the same calendar) so the dynamic block
// stays byte-stable within a day. That byte-stability is what preserves the
// trailing-message prompt cache: a glance that changed every turn would shift
// the system-prompt prefix and force a cache-creation on the trailing markers
// each turn (see .claude/rules/prompt-cache.md — the dynamic block is meant to
// be byte-stable except the midnight date rollover).
//
// Cost: at most one bounded calendar fetch per day; every other turn reuses the
// frozen string. The live `calendar` tool stays authoritative for fresh detail —
// this is background awareness only, deliberately allowed to be a little stale.
package chat

import (
	"context"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

// CalendarGlanceFunc returns the ambient upcoming-events glance for a turn.
// Empty string = no events / unavailable → caller injects no section. A nil
// CalendarGlanceFunc disables the feature entirely.
type CalendarGlanceFunc func(ctx context.Context, sessionKey, tz string) string

const (
	calGlanceDays         = 3 // today + next 2 days
	calGlanceFetchTimeout = 3 * time.Second
)

// calGlanceCache freezes the formatted glance per calendar day. Single-user
// deployment → one global slot keyed by date suffices (sessions share it).
var calGlanceCache = struct {
	mu    sync.Mutex
	date  string // "2006-01-02" in display tz
	value string
	built bool
}{}

// NewCalendarGlanceFunc returns a glance provider over the given calendar deps,
// or nil when no calendar source is wired (so the Handler leaves the feature
// off). Cheap on cache hits; at most one bounded calendar fetch per day.
func NewCalendarGlanceFunc(d *toolctx.CalendarDeps) CalendarGlanceFunc {
	if d == nil || (d.Client == nil && d.Local == nil) {
		return nil
	}
	return func(ctx context.Context, _ string, tz string) string {
		loc := glanceLocation(tz)
		now := time.Now().In(loc)
		today := now.Format("2006-01-02")

		calGlanceCache.mu.Lock()
		if calGlanceCache.built && calGlanceCache.date == today {
			v := calGlanceCache.value
			calGlanceCache.mu.Unlock()
			return v
		}
		calGlanceCache.mu.Unlock()

		// Build outside the lock — the fetch may block up to the timeout and we
		// don't want to stall other turns. Concurrent builders are harmless
		// (idempotent; last write wins).
		fctx, cancel := context.WithTimeout(ctx, calGlanceFetchTimeout)
		glance := tools.CalendarGlance(fctx, d, now, calGlanceDays)
		cancel()

		calGlanceCache.mu.Lock()
		calGlanceCache.date = today
		calGlanceCache.value = glance
		calGlanceCache.built = true
		calGlanceCache.mu.Unlock()
		return glance
	}
}

// glanceLocation resolves a tz name to a *time.Location, falling back to KST
// (the single-user deployment tz) when empty or unknown.
func glanceLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if loc, err := time.LoadLocation("Asia/Seoul"); err == nil {
		return loc
	}
	return time.FixedZone("KST", 9*60*60)
}
