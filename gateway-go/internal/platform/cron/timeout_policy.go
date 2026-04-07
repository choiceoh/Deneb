// timeout_policy.go — Resolves per-job execution timeouts.
// Mirrors src/cron/service/timeout-policy.ts (25 LOC).
package cron

const (
	// DefaultJobTimeoutMs is the default timeout for systemEvent jobs (10 minutes).
	DefaultJobTimeoutMs = 10 * 60 * 1000

	// AgentTurnSafetyTimeoutMs is the safety ceiling for agentTurn jobs (60 minutes).
	// Agent turns can legitimately run much longer than generic cron jobs.
	AgentTurnSafetyTimeoutMs = 60 * 60 * 1000
)

// ResolveCronJobTimeoutMs returns the effective timeout for a cron job in milliseconds.
// Uses the job's configured timeoutSeconds if set, otherwise falls back to
// AgentTurnSafetyTimeoutMs for agentTurn or DefaultJobTimeoutMs for systemEvent.
func ResolveCronJobTimeoutMs(job StoreJob) int64 {
	if job.Payload.Kind == "agentTurn" && job.Payload.TimeoutSeconds > 0 {
		return int64(job.Payload.TimeoutSeconds) * 1000
	}
	if job.Payload.Kind == "agentTurn" {
		return AgentTurnSafetyTimeoutMs
	}
	return DefaultJobTimeoutMs
}
