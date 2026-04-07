// validate_timestamp.go — Validates "at" schedule timestamps.
// Mirrors src/cron/validate-timestamp.ts (66 LOC).
package cron

import (
	"fmt"
	"time"
)

const (
	oneMinuteMs = 60 * 1000
	tenYearsMs  = int64(10 * 365.25 * 24 * 60 * 60 * 1000)
)

// TimestampValidationResult reports whether a schedule timestamp is valid.
type TimestampValidationResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// ValidateScheduleTimestamp validates an "at" schedule's timestamp.
// Rejects timestamps that are:
//   - More than 1 minute in the past
//   - More than 10 years in the future
func ValidateScheduleTimestamp(schedule StoreSchedule, nowMs int64) TimestampValidationResult {
	if schedule.Kind != "at" {
		return TimestampValidationResult{OK: true}
	}

	atMs := parseAbsoluteTimeMs(schedule.At)
	if atMs <= 0 {
		return TimestampValidationResult{
			OK:      false,
			Message: fmt.Sprintf("Invalid schedule.at: expected ISO-8601 timestamp (got %s)", schedule.At),
		}
	}

	diffMs := atMs - nowMs

	// Check if timestamp is in the past (allow 1 minute grace).
	if diffMs < -oneMinuteMs {
		nowDate := time.UnixMilli(nowMs).UTC().Format(time.RFC3339)
		atDate := time.UnixMilli(atMs).UTC().Format(time.RFC3339)
		minutesAgo := -diffMs / oneMinuteMs
		return TimestampValidationResult{
			OK:      false,
			Message: fmt.Sprintf("schedule.at is in the past: %s (%d minutes ago). Current time: %s", atDate, minutesAgo, nowDate),
		}
	}

	// Check if timestamp is too far in the future.
	if diffMs > tenYearsMs {
		atDate := time.UnixMilli(atMs).UTC().Format(time.RFC3339)
		yearsAhead := diffMs / (365.25 * 24 * 60 * 60 * 1000)
		return TimestampValidationResult{
			OK:      false,
			Message: fmt.Sprintf("schedule.at is too far in the future: %s (%d years ahead). Maximum allowed: 10 years", atDate, yearsAhead),
		}
	}

	return TimestampValidationResult{OK: true}
}
