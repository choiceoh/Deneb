package tasks

import "sort"

// AuditSeverity classifies the urgency of an audit finding.
type AuditSeverity string

const (
	SeverityWarning AuditSeverity = "warning"
	SeverityError   AuditSeverity = "error"
)

// AuditCode identifies the type of audit finding.
type AuditCode string

const (
	AuditStaleQueued            AuditCode = "stale_queued"
	AuditStaleRunning           AuditCode = "stale_running"
	AuditLost                   AuditCode = "lost"
	AuditDeliveryFailed         AuditCode = "delivery_failed"
	AuditMissingCleanup         AuditCode = "missing_cleanup"
	AuditInconsistentTimestamps AuditCode = "inconsistent_timestamps"
)

// Default thresholds.
const (
	DefaultStaleQueuedMs  int64 = 10 * 60 * 1000 // 10 minutes
	DefaultStaleRunningMs int64 = 30 * 60 * 1000 // 30 minutes
)

// AuditFinding is a single audit result.
type AuditFinding struct {
	Severity AuditSeverity `json:"severity"`
	Code     AuditCode     `json:"code"`
	TaskID   string        `json:"taskId"`
	Detail   string        `json:"detail"`
	AgeMs    int64         `json:"ageMs,omitempty"`
}

// AuditSummary aggregates audit findings.
type AuditSummary struct {
	Total    int               `json:"total"`
	Errors   int               `json:"errors"`
	Warnings int               `json:"warnings"`
	ByCode   map[AuditCode]int `json:"byCode"`
	Findings []*AuditFinding   `json:"findings"`
}

// AuditOptions configures audit behavior.
type AuditOptions struct {
	Now            int64
	StaleQueuedMs  int64
	StaleRunningMs int64
}

// RunAudit examines all tasks in the registry for issues.
func RunAudit(reg *Registry, opts AuditOptions) *AuditSummary {
	if opts.Now == 0 {
		opts.Now = NowMs()
	}
	if opts.StaleQueuedMs == 0 {
		opts.StaleQueuedMs = DefaultStaleQueuedMs
	}
	if opts.StaleRunningMs == 0 {
		opts.StaleRunningMs = DefaultStaleRunningMs
	}

	tasks := reg.ListAll()
	var findings []*AuditFinding

	for _, t := range tasks {
		ref := taskReferenceAt(t)
		age := opts.Now - ref

		switch t.Status {
		case StatusQueued:
			if age >= opts.StaleQueuedMs {
				findings = append(findings, &AuditFinding{
					Severity: SeverityWarning,
					Code:     AuditStaleQueued,
					TaskID:   t.TaskID,
					Detail:   "task queued for too long without starting",
					AgeMs:    age,
				})
			}

		case StatusRunning:
			if age >= opts.StaleRunningMs {
				findings = append(findings, &AuditFinding{
					Severity: SeverityError,
					Code:     AuditStaleRunning,
					TaskID:   t.TaskID,
					Detail:   "task running for too long without updates",
					AgeMs:    age,
				})
			}

		case StatusLost:
			findings = append(findings, &AuditFinding{
				Severity: SeverityError,
				Code:     AuditLost,
				TaskID:   t.TaskID,
				Detail:   "task lost (backing session disappeared)",
				AgeMs:    age,
			})
		}

		// Check delivery failures on non-silent tasks.
		if t.DeliveryStatus == DeliveryFailed && t.NotifyPolicy != NotifySilent {
			findings = append(findings, &AuditFinding{
				Severity: SeverityWarning,
				Code:     AuditDeliveryFailed,
				TaskID:   t.TaskID,
				Detail:   "task delivery failed",
			})
		}

		// Terminal tasks should have cleanup scheduled.
		if t.Status.IsTerminal() && t.CleanupAfter == 0 {
			findings = append(findings, &AuditFinding{
				Severity: SeverityWarning,
				Code:     AuditMissingCleanup,
				TaskID:   t.TaskID,
				Detail:   "terminal task missing cleanup timestamp",
			})
		}

		// Timestamp consistency checks.
		if ts := checkTimestamps(t); ts != "" {
			findings = append(findings, &AuditFinding{
				Severity: SeverityWarning,
				Code:     AuditInconsistentTimestamps,
				TaskID:   t.TaskID,
				Detail:   ts,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity == SeverityError
		}
		return findings[i].AgeMs > findings[j].AgeMs
	})

	sum := &AuditSummary{
		Total:    len(findings),
		ByCode:   make(map[AuditCode]int),
		Findings: findings,
	}
	for _, f := range findings {
		sum.ByCode[f.Code]++
		if f.Severity == SeverityError {
			sum.Errors++
		} else {
			sum.Warnings++
		}
	}
	return sum
}

func taskReferenceAt(t *TaskRecord) int64 {
	if t.LastEventAt > 0 {
		return t.LastEventAt
	}
	if t.StartedAt > 0 {
		return t.StartedAt
	}
	return t.CreatedAt
}

func checkTimestamps(t *TaskRecord) string {
	if t.StartedAt > 0 && t.StartedAt < t.CreatedAt {
		return "startedAt earlier than createdAt"
	}
	if t.EndedAt > 0 && t.StartedAt > 0 && t.EndedAt < t.StartedAt {
		return "endedAt earlier than startedAt"
	}
	if t.Status.IsActive() && t.EndedAt > 0 {
		return "active task has endedAt set"
	}
	return ""
}
