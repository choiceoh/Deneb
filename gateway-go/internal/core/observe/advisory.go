// advisory.go — the "this line reports an observation, not a fault" predicate.
//
// It lives beside LogLine because two independent self-diagnosis lanes need the
// same answer and reached different ones. genesis's runtime-error mining has
// filtered these since 2026-07-25, after `regression-watch: regression detected
// (observe-only)` produced a skill candidate that landed with a no_effect
// verdict — there was nothing to fix. anomaly-watch never learned it: on the
// 2026-08-30 ledger, 3 of its 7 HIGH-severity findings were built on
// observe-only regression-watch lines, reporting a lane working exactly as
// designed as the most serious thing in the window.
package observe

import "regexp"

// advisoryPattern matches an EXPLICIT authorial marker that a line reports an
// observation rather than a fault.
//
// Deliberately narrow: it respects a marker the AUTHOR wrote, and does not try
// to infer "this mechanism is working as designed". That inference would
// collide head-on with why WARN lines are admitted at all — graceful
// degradation DOWNGRADES real defects, so "model failed, trying fallback" is a
// working fallback AND a real upstream defect signal. Only the explicit marker
// is safe to trust.
var advisoryPattern = regexp.MustCompile(
	`(?i)\(observe.?only\)|\bobserve.?only\b|\badvisory\b|\bdry.?run\b|\bwill recover\b|\bno-?op\b`,
)

// IsAdvisory reports whether a log line is an intentionally advisory
// observation rather than a fault worth diagnosing.
//
// Checked on the message only: `error` attrs carry the underlying failure text,
// where these words would be coincidental.
func IsAdvisory(line LogLine) bool {
	return advisoryPattern.MatchString(line.Msg)
}

// IsAdvisoryMessage is IsAdvisory for a bare message string.
func IsAdvisoryMessage(msg string) bool {
	return advisoryPattern.MatchString(msg)
}
