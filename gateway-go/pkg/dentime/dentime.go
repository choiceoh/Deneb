// Package dentime is Deneb's timezone-aware clock.
//
// Deneb is a Korean-first single-user deployment where operators want
// deterministic wall-clock time (typically KST) for logs, cron entries, and
// user-facing timestamps. Callers use dentime.Now() instead of time.Now() in
// paths where the zone affects what the user sees.
//
// Resolution order (first non-empty wins):
//
//  1. DENEB_TIMEZONE env var (IANA name: "Asia/Seoul", "UTC", "America/Los_Angeles")
//  2. Config-supplied zone (via SetConfigTimezone, read from deneb.json's
//     top-level "timezone" key at bootstrap)
//  3. Server local time (time.Local)
//
// Invalid zone strings log a one-time warning via slog.Default() and fall
// through to the next source. Resolution is performed on first access and
// then cached; SetConfigTimezone and ResetCache force re-resolution.
//
// Do NOT use dentime for metrics, latency measurements, or any monotonic
// comparison — those must keep using time.Now() so the monotonic clock reading
// is preserved. Prefer dentime only for values that reach logs, persisted
// timestamps formatted for humans, or cron/schedule display.
//
// Thread-safety: all exported functions are safe for concurrent use.
package dentime

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// envVar is the environment variable checked first for an IANA zone name.
const envVar = "DENEB_TIMEZONE"

var (
	mu        sync.RWMutex
	resolved  bool
	cached    *time.Location // nil means "use time.Local"
	effective string         // zone name actually used (e.g. "Asia/Seoul" or "Local")
	configTZ  string         // zone name set via SetConfigTimezone (trimmed)

	// warnedZones records zone names that already produced a fallback warning,
	// so the same bad value does not spam the log on every reset.
	warnedMu sync.Mutex
	warned   = map[string]struct{}{}
)

// Now returns the current time in the resolved zone. Equivalent to time.Now()
// when no zone is configured and the server local zone is used.
func Now() time.Time {
	return time.Now().In(Location())
}

// Location returns the resolved *time.Location. Returns time.Local when no
// zone is configured or the configured zone is invalid. Never returns nil.
func Location() *time.Location {
	mu.RLock()
	if resolved {
		loc := cached
		mu.RUnlock()
		if loc != nil {
			return loc
		}
		return time.Local
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()
	if !resolved {
		cached, effective = resolveLocked()
		resolved = true
	}
	if cached != nil {
		return cached
	}
	return time.Local
}

// Name returns the effective zone name. Possible values:
//   - a successfully loaded IANA name (e.g. "Asia/Seoul", "UTC")
//   - "Local" when no zone was configured, or the configured zone was invalid
//     and we fell back to the server local zone.
func Name() string {
	// Ensure resolution has run.
	_ = Location()
	mu.RLock()
	defer mu.RUnlock()
	if effective != "" {
		return effective
	}
	return "Local"
}

// SetConfigTimezone stores a config-derived IANA zone name. The env var, when
// present, still wins. Calling this resets the cache so the next Now()/Location()
// call picks up the change. Pass "" to clear a previously set config value.
func SetConfigTimezone(name string) {
	trimmed := strings.TrimSpace(name)
	mu.Lock()
	configTZ = trimmed
	resolved = false
	cached = nil
	effective = ""
	mu.Unlock()
}

// ResetCache forces re-resolution on the next Now()/Location()/Name() call.
// Intended for tests and for callers that have mutated the environment at
// runtime. Does NOT clear a previously set config zone — use
// SetConfigTimezone("") for that.
func ResetCache() {
	mu.Lock()
	resolved = false
	cached = nil
	effective = ""
	mu.Unlock()

	warnedMu.Lock()
	warned = map[string]struct{}{}
	warnedMu.Unlock()
}

// resolveLocked computes the effective location using the documented precedence.
// Caller must hold mu (write). Returns (loc, name). loc may be nil,
// meaning "use time.Local"; name in that case will be "Local".
func resolveLocked() (loc *time.Location, name string) {
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		if l, ok := loadZone(env); ok {
			n := l.String()
			return l, n
		}
		// Fall through to config / local on invalid env.
	}
	if configTZ != "" {
		if l, ok := loadZone(configTZ); ok {
			n := l.String()
			return l, n
		}
		// Fall through to local on invalid config.
	}
	return nil, "Local"
}

// loadZone validates an IANA zone name. Returns (nil, false) if the zone is
// invalid; logs a one-time warning per-name. time.LoadLocation accepts "" and
// "UTC" specially — we treat "" as invalid so the precedence chain continues.
func loadZone(name string) (*time.Location, bool) {
	if name == "" {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		warnOnce(name, err)
		return nil, false
	}
	return loc, true
}

// warnOnce emits a single slog.Warn per invalid zone name.
func warnOnce(name string, err error) {
	warnedMu.Lock()
	if _, seen := warned[name]; seen {
		warnedMu.Unlock()
		return
	}
	warned[name] = struct{}{}
	warnedMu.Unlock()
	slog.Default().Warn("dentime: invalid timezone, falling back",
		"timezone", name,
		"error", err,
	)
}
