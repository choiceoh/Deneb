package cron

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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
func evaluateCronExpr(expr string, now time.Time, _ *time.Location) time.Time {
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
func parseCronField(field string, lo, hi int) map[int]bool {
	result := make(map[int]bool)

	parts := strings.Split(field, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "*" {
			for i := lo; i <= hi; i++ {
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
			rangeStart, rangeEnd := lo, hi
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
	return int64(val % uint64(staggerMs)) //nolint:gosec // G115 — result is bounded by staggerMs which fits in int64
}

// FormatHumanSchedule converts a StoreSchedule to a Korean-friendly display string.
func FormatHumanSchedule(s StoreSchedule) string {
	switch s.Kind {
	case "at":
		if s.At == "" {
			return "일회성"
		}
		if t := parseAbsoluteTimeMs(s.At); t > 0 {
			return time.UnixMilli(t).Format("2006-01-02 15:04") + " (일회성)"
		}
		return s.At + " (일회성)"

	case "every":
		if s.EveryMs <= 0 {
			return "반복"
		}
		desc := FormatDurationKorean(s.EveryMs)
		if s.AnchorMs > 0 {
			anchor := time.UnixMilli(s.AnchorMs).Format("15:04")
			return desc + "마다 (" + anchor + " 기준)"
		}
		return desc + "마다"

	case "cron":
		desc := formatCronExprKorean(s.Expr)
		tz := s.Tz
		if tz == "" {
			tz = "local"
		}
		if tz != "local" && tz != time.Local.String() {
			return desc + " (" + shortTzName(tz) + ")"
		}
		return desc

	default:
		return s.Kind
	}
}

// formatCronExprKorean converts common cron expressions to Korean descriptions.
func formatCronExprKorean(expr string) string {
	lower := strings.ToLower(strings.TrimSpace(expr))

	// Shorthand aliases.
	switch lower {
	case "@yearly", "@annually":
		return "매년 1월 1일 00:00"
	case "@monthly":
		return "매월 1일 00:00"
	case "@weekly":
		return "매주 일요일 00:00"
	case "@daily", "@midnight":
		return "매일 00:00"
	case "@hourly":
		return "매시 정각"
	}

	fields := strings.Fields(lower)
	if len(fields) < 5 {
		return "cron: " + expr
	}

	minute, hour, dom, mon, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// "every N minutes": */N * * * *
	if strings.HasPrefix(minute, "*/") && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		if n, err := strconv.Atoi(minute[2:]); err == nil {
			return fmt.Sprintf("%d분마다", n)
		}
	}

	// "every N hours": 0 */N * * *
	if minute == "0" && strings.HasPrefix(hour, "*/") && dom == "*" && mon == "*" && dow == "*" {
		if n, err := strconv.Atoi(hour[2:]); err == nil {
			return fmt.Sprintf("%d시간마다", n)
		}
	}

	// Fixed minute + hour patterns.
	minVal, minErr := strconv.Atoi(minute)
	hourVal, hourErr := strconv.Atoi(hour)
	fixedTime := minErr == nil && hourErr == nil

	if fixedTime {
		timeStr := fmt.Sprintf("%02d:%02d", hourVal, minVal)

		// "daily at HH:MM": M H * * *
		if dom == "*" && mon == "*" && dow == "*" {
			return "매일 " + timeStr
		}

		// "weekdays at HH:MM": M H * * 1-5
		if dom == "*" && mon == "*" && (dow == "1-5" || dow == "mon-fri") {
			return "평일 " + timeStr
		}

		// "weekends": M H * * 0,6 or 6,0
		if dom == "*" && mon == "*" && (dow == "0,6" || dow == "6,0" || dow == "sat,sun" || dow == "sun,sat") {
			return "주말 " + timeStr
		}

		// "weekly on specific day": M H * * D
		if dom == "*" && mon == "*" {
			if dayName := dowKorean(dow); dayName != "" {
				return "매주 " + dayName + " " + timeStr
			}
		}

		// "monthly on Nth": M H N * *
		if _, domErr := strconv.Atoi(dom); domErr == nil && mon == "*" && dow == "*" {
			return fmt.Sprintf("매월 %s일 %s", dom, timeStr)
		}
	}

	// "hourly at minute M": M * * * *
	if minErr == nil && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		if minVal == 0 {
			return "매시 정각"
		}
		return fmt.Sprintf("매시 %d분", minVal)
	}

	return "cron: " + expr
}

// dowKorean maps a single day-of-week value to Korean.
func dowKorean(dow string) string {
	switch strings.ToLower(dow) {
	case "0", "sun":
		return "일요일"
	case "1", "mon":
		return "월요일"
	case "2", "tue":
		return "화요일"
	case "3", "wed":
		return "수요일"
	case "4", "thu":
		return "목요일"
	case "5", "fri":
		return "금요일"
	case "6", "sat":
		return "토요일"
	default:
		return ""
	}
}

// shortTzName returns a short display name for a timezone.
func shortTzName(tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return tz
	}
	name, _ := time.Now().In(loc).Zone()
	return name
}

// FormatDurationKorean formats milliseconds as a Korean duration string.
func FormatDurationKorean(ms int64) string {
	if ms <= 0 {
		return "0초"
	}
	d := time.Duration(ms) * time.Millisecond

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d일", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d시간", hours))
	}
	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d분", mins))
	}
	if secs > 0 && days == 0 && hours == 0 {
		parts = append(parts, fmt.Sprintf("%d초", secs))
	}
	if len(parts) == 0 {
		return "0초"
	}
	return strings.Join(parts, " ")
}

// FormatRelativeTime formats a timestamp relative to now in Korean.
func FormatRelativeTime(targetMs int64) string {
	now := time.Now().UnixMilli()
	diff := targetMs - now

	if diff > 0 {
		return FormatDurationKorean(diff) + " 후"
	}
	if diff < 0 {
		return FormatDurationKorean(-diff) + " 전"
	}
	return "지금"
}

// --- Smart schedule parsing ---

// SmartScheduleOpts holds optional parameters for ParseSmartScheduleWithOpts.
type SmartScheduleOpts struct {
	Tz         string // timezone (e.g. "Asia/Seoul") — applied to cron kind
	StaggerMs  int64  // stagger window in ms — applied to cron kind
	AnchorTime string // anchor time (ISO 8601) — applied to every kind
}

// ParseSmartSchedule parses a schedule spec into a StoreSchedule, auto-detecting the kind:
//   - Interval: "1h", "30m", "every 5m", raw milliseconds → kind="every"
//   - Cron expression: "0 8 * * *", "@daily", "@hourly" → kind="cron"
//   - Timestamp: ISO 8601 ("2026-04-06T08:00:00") → kind="at"
func ParseSmartSchedule(spec string) (StoreSchedule, error) {
	return ParseSmartScheduleWithOpts(spec, SmartScheduleOpts{})
}

// ParseSmartScheduleWithOpts is like ParseSmartSchedule but accepts additional options
// for timezone, stagger, and anchor time.
func ParseSmartScheduleWithOpts(spec string, opts SmartScheduleOpts) (StoreSchedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return StoreSchedule{}, fmt.Errorf("empty schedule specification")
	}

	// Validate timezone upfront if provided.
	if opts.Tz != "" {
		if _, err := time.LoadLocation(opts.Tz); err != nil {
			return StoreSchedule{}, fmt.Errorf("invalid timezone %q: %w", opts.Tz, err)
		}
	}

	// 1. Cron shorthand aliases (@daily, @hourly, etc.)
	lower := strings.ToLower(spec)
	switch lower {
	case "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly":
		s := StoreSchedule{Kind: "cron", Expr: lower, Tz: opts.Tz, StaggerMs: opts.StaggerMs}
		return s, nil
	}

	// 2. Looks like a cron expression (5 space-separated fields starting with digit or *)
	fields := strings.Fields(spec)
	if len(fields) == 5 && looksLikeCronExpr(fields) {
		loc := time.Local
		if opts.Tz != "" {
			loc, _ = time.LoadLocation(opts.Tz) // best-effort: defaults to Local
		}
		now := time.Now()
		next := evaluateCronExpr(spec, now, loc)
		if next.IsZero() {
			return StoreSchedule{}, fmt.Errorf("invalid cron expression %q: no matching time found in next 366 days", spec)
		}
		s := StoreSchedule{Kind: "cron", Expr: spec, Tz: opts.Tz, StaggerMs: opts.StaggerMs}
		return s, nil
	}

	// 3. ISO 8601 timestamp → kind="at"
	if ts := parseAbsoluteTimeMs(spec); ts > 0 {
		if strings.Contains(spec, "T") || strings.Contains(spec, "-") {
			return StoreSchedule{Kind: "at", At: spec}, nil
		}
	}

	// 4. Interval: "every Xunit", Go duration, raw ms.
	intervalMs, err := parseIntervalMs(spec)
	if err != nil {
		return StoreSchedule{}, err
	}
	s := StoreSchedule{Kind: "every", EveryMs: intervalMs}
	// Apply anchor time for intervals.
	if opts.AnchorTime != "" {
		anchorMs := parseAbsoluteTimeMs(opts.AnchorTime)
		if anchorMs <= 0 {
			return StoreSchedule{}, fmt.Errorf("invalid anchor time %q", opts.AnchorTime)
		}
		s.AnchorMs = anchorMs
	}
	return s, nil
}

// looksLikeCronExpr returns true if the 5 fields look like a cron expression.
func looksLikeCronExpr(fields []string) bool {
	for _, f := range fields {
		f = strings.ToLower(f)
		for _, ch := range f {
			if ch >= '0' && ch <= '9' {
				continue
			}
			switch ch {
			case '*', ',', '-', '/':
				continue
			}
			// Allow month/day names (a-z).
			if ch >= 'a' && ch <= 'z' {
				continue
			}
			return false
		}
	}
	return true
}

// parseIntervalMs parses a human-readable interval into milliseconds.
// Supports: raw ms ("5000"), "every Xunit" ("every 5m"), Go duration ("30s").
func parseIntervalMs(spec string) (int64, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, fmt.Errorf("empty schedule specification")
	}

	// Try raw milliseconds.
	if ms, err := strconv.ParseInt(spec, 10, 64); err == nil && ms > 0 {
		return ms, nil
	}

	// Try "every Xunit" format.
	lower := strings.ToLower(spec)
	if strings.HasPrefix(lower, "every ") {
		durStr := strings.TrimSpace(lower[6:])
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", durStr, err)
		}
		if dur <= 0 {
			return 0, fmt.Errorf("schedule duration must be positive")
		}
		return dur.Milliseconds(), nil
	}

	// Try Go duration directly.
	dur, err := time.ParseDuration(spec)
	if err != nil {
		return 0, fmt.Errorf("unrecognized schedule format %q", spec)
	}
	if dur <= 0 {
		return 0, fmt.Errorf("schedule duration must be positive")
	}
	return dur.Milliseconds(), nil
}
