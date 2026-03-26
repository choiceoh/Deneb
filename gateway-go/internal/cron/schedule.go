package cron

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"time"
)

// DefaultTopOfHourStaggerMs is the default stagger for top-of-hour cron jobs.
const DefaultTopOfHourStaggerMs = 5 * 60 * 1000 // 5 minutes

// monthNames maps month abbreviations to numbers for cron fields.
var monthNames = map[string]string{
	"jan": "1", "feb": "2", "mar": "3", "apr": "4",
	"may": "5", "jun": "6", "jul": "7", "aug": "8",
	"sep": "9", "oct": "10", "nov": "11", "dec": "12",
}

// dowNames maps day-of-week abbreviations to numbers for cron fields.
var dowNames = map[string]string{
	"sun": "0", "mon": "1", "tue": "2", "wed": "3",
	"thu": "4", "fri": "5", "sat": "6",
}

// resolveNamedValues replaces named month/dow values in a cron field.
func resolveNamedValues(field string, names map[string]string) string {
	lower := strings.ToLower(field)
	for name, num := range names {
		lower = strings.ReplaceAll(lower, name, num)
	}
	return lower
}

// ComputeNextRunAtMs computes the next run time for a schedule.
func ComputeNextRunAtMs(schedule StoreSchedule, nowMs int64) int64 {
	switch schedule.Kind {
	case "at":
		return computeNextAtMs(schedule, nowMs)
	case "every":
		return computeNextEveryMs(schedule, nowMs)
	case "cron":
		return computeNextCronMs(schedule, nowMs)
	default:
		return 0
	}
}

func computeNextAtMs(schedule StoreSchedule, nowMs int64) int64 {
	atMs := parseAbsoluteTimeMs(schedule.At)
	if atMs <= 0 {
		return 0
	}
	if atMs > nowMs {
		return atMs
	}
	return 0 // already past
}

func computeNextEveryMs(schedule StoreSchedule, nowMs int64) int64 {
	everyMs := schedule.EveryMs
	if everyMs <= 0 {
		return 0
	}
	anchor := schedule.AnchorMs
	if anchor <= 0 {
		anchor = nowMs
	}
	if nowMs < anchor {
		return anchor
	}
	elapsed := nowMs - anchor
	steps := (elapsed + everyMs - 1) / everyMs
	if steps < 1 {
		steps = 1
	}
	return anchor + steps*everyMs
}

// computeNextCronMs evaluates a cron expression to find the next run time.
// Uses a simplified approach: parses common cron patterns natively.
// For complex patterns, falls back to interval-based approximation.
func computeNextCronMs(schedule StoreSchedule, nowMs int64) int64 {
	expr := strings.TrimSpace(schedule.Expr)
	if expr == "" {
		return 0
	}

	// Resolve timezone.
	tz := strings.TrimSpace(schedule.Tz)
	var loc *time.Location
	if tz != "" {
		var err error
		loc, err = time.LoadLocation(tz)
		if err != nil {
			loc = time.UTC
		}
	} else {
		loc = time.Local
	}

	now := time.UnixMilli(nowMs).In(loc)

	// Parse the cron expression.
	nextTime := evaluateCronExpr(expr, now, loc)
	if nextTime.IsZero() {
		return 0
	}
	nextMs := nextTime.UnixMilli()

	// Apply stagger offset.
	staggerMs := ResolveCronStaggerMs(schedule)
	if staggerMs > 0 {
		offset := stableJobOffset(schedule.Expr, staggerMs)
		nextMs += offset
	}

	if nextMs <= nowMs {
		return 0
	}
	return nextMs
}

// evaluateCronExpr parses a standard 5-field cron expression and finds the
// next matching time after `now`. Supports: *, ranges (1-5), steps (*/5),
// lists (1,3,5), fixed values, @shorthand aliases, and named months/days.
func evaluateCronExpr(expr string, now time.Time, loc *time.Location) time.Time {
	// Expand shorthand aliases.
	switch strings.ToLower(strings.TrimSpace(expr)) {
	case "@yearly", "@annually":
		expr = "0 0 1 1 *"
	case "@monthly":
		expr = "0 0 1 * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@hourly":
		expr = "0 * * * *"
	}

	fields := strings.Fields(expr)
	if len(fields) < 5 {
		return time.Time{}
	}

	// Resolve named month/dow values (JAN-DEC, MON-SUN).
	fields[3] = resolveNamedValues(fields[3], monthNames)
	fields[4] = resolveNamedValues(fields[4], dowNames)

	// Parse: minute hour dom month dow
	minutes := parseCronField(fields[0], 0, 59)
	hours := parseCronField(fields[1], 0, 23)
	doms := parseCronField(fields[2], 1, 31)
	months := parseCronField(fields[3], 1, 12)
	dows := parseCronField(fields[4], 0, 6)

	if minutes == nil || hours == nil || doms == nil || months == nil || dows == nil {
		return time.Time{}
	}

	// Brute-force search: check every minute for the next 366 days.
	candidate := now.Truncate(time.Minute).Add(time.Minute)
	limit := candidate.Add(366 * 24 * time.Hour)

	for candidate.Before(limit) {
		m := candidate.Minute()
		h := candidate.Hour()
		dom := candidate.Day()
		mon := int(candidate.Month())
		dow := int(candidate.Weekday()) // Sunday=0

		if months[mon] && doms[dom] && dows[dow] && hours[h] && minutes[m] {
			return candidate
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}
}

// parseCronField parses a single cron field into a boolean set.
// Returns nil on parse error.
func parseCronField(field string, min, max int) map[int]bool {
	result := make(map[int]bool)

	parts := strings.Split(field, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "*" {
			for i := min; i <= max; i++ {
				result[i] = true
			}
			continue
		}

		// Step: */N or M-N/S
		if strings.Contains(part, "/") {
			tokens := strings.SplitN(part, "/", 2)
			step, err := strconv.Atoi(tokens[1])
			if err != nil || step <= 0 {
				return nil
			}
			rangeStart, rangeEnd := min, max
			if tokens[0] != "*" {
				rangeParts := strings.SplitN(tokens[0], "-", 2)
				rangeStart, err = strconv.Atoi(rangeParts[0])
				if err != nil {
					return nil
				}
				if len(rangeParts) > 1 {
					rangeEnd, err = strconv.Atoi(rangeParts[1])
					if err != nil {
						return nil
					}
				}
			}
			for i := rangeStart; i <= rangeEnd; i += step {
				result[i] = true
			}
			continue
		}

		// Range: M-N
		if strings.Contains(part, "-") {
			tokens := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(tokens[0])
			if err != nil {
				return nil
			}
			end, err := strconv.Atoi(tokens[1])
			if err != nil {
				return nil
			}
			for i := start; i <= end; i++ {
				result[i] = true
			}
			continue
		}

		// Fixed value.
		val, err := strconv.Atoi(part)
		if err != nil {
			return nil
		}
		result[val] = true
	}
	return result
}

// parseAbsoluteTimeMs parses an absolute time string into milliseconds since epoch.
// Supports ISO8601 timestamps and raw millisecond numbers.
func parseAbsoluteTimeMs(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Try as number first.
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 0 {
		return ms
	}
	if ms, err := strconv.ParseFloat(s, 64); err == nil && ms > 0 && !math.IsInf(ms, 0) {
		return int64(ms)
	}
	// Try as ISO8601.
	for _, format := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(format, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// IsRecurringTopOfHourCronExpr returns true if the cron expression fires at
// the top of every hour (e.g., "0 * * * *").
func IsRecurringTopOfHourCronExpr(expr string) bool {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) == 5 {
		return fields[0] == "0" && strings.Contains(fields[1], "*")
	}
	if len(fields) == 6 {
		return fields[0] == "0" && fields[1] == "0" && strings.Contains(fields[2], "*")
	}
	return false
}

// ResolveCronStaggerMs returns the effective stagger for a cron schedule.
func ResolveCronStaggerMs(schedule StoreSchedule) int64 {
	if schedule.StaggerMs > 0 {
		return schedule.StaggerMs
	}
	if IsRecurringTopOfHourCronExpr(schedule.Expr) {
		return DefaultTopOfHourStaggerMs
	}
	return 0
}

// stableJobOffset computes a deterministic offset for a job based on its
// expression, using SHA256. This distributes cron jobs across the stagger window.
func stableJobOffset(expr string, staggerMs int64) int64 {
	if staggerMs <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(expr))
	val := binary.BigEndian.Uint64(h[:8])
	return int64(val % uint64(staggerMs))
}
