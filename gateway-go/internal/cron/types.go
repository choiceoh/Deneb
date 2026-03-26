// types.go — Full cron type definitions matching src/cron/types.ts + types-shared.ts.
package cron

// CronDeliveryMode defines how cron output is delivered.
type CronDeliveryMode string

const (
	DeliveryModeNone     CronDeliveryMode = "none"
	DeliveryModeAnnounce CronDeliveryMode = "announce"
	DeliveryModeWebhook  CronDeliveryMode = "webhook"
)

// CronSessionTarget specifies which session to run the job in.
type CronSessionTarget string

const (
	SessionTargetMain     CronSessionTarget = "main"
	SessionTargetIsolated CronSessionTarget = "isolated"
	SessionTargetCurrent  CronSessionTarget = "current"
)

// CronWakeMode controls post-execution wake behavior.
type CronWakeMode string

const (
	WakeModeNextHeartbeat CronWakeMode = "next-heartbeat"
	WakeModeNow           CronWakeMode = "now"
)

// CronRunStatus is the outcome of a cron job run.
type CronRunStatus string

const (
	RunStatusOk      CronRunStatus = "ok"
	RunStatusError   CronRunStatus = "error"
	RunStatusSkipped CronRunStatus = "skipped"
)

// CronDeliveryStatus tracks the delivery outcome.
type CronDeliveryStatus string

const (
	DeliveryStatusDelivered    CronDeliveryStatus = "delivered"
	DeliveryStatusNotDelivered CronDeliveryStatus = "not-delivered"
	DeliveryStatusUnknown      CronDeliveryStatus = "unknown"
	DeliveryStatusNotRequested CronDeliveryStatus = "not-requested"
)

// CronFailureDestination configures where failure alerts are sent.
type CronFailureDestination struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	Mode      string `json:"mode,omitempty"` // "announce" or "webhook"
}

// CronDeliveryFull is the full delivery configuration (extends JobDeliveryConfig).
type CronDeliveryFull struct {
	Mode               CronDeliveryMode        `json:"mode"`
	Channel            string                  `json:"channel,omitempty"`
	To                 string                  `json:"to,omitempty"`
	AccountID          string                  `json:"accountId,omitempty"`
	BestEffort         bool                    `json:"bestEffort,omitempty"`
	FailureDestination *CronFailureDestination `json:"failureDestination,omitempty"`
}

// CronFailureAlert configures failure notification behavior.
type CronFailureAlert struct {
	After      int    `json:"after,omitempty"`
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	CooldownMs int64  `json:"cooldownMs,omitempty"`
	Mode       string `json:"mode,omitempty"`    // "announce" or "webhook"
	AccountID  string `json:"accountId,omitempty"`
}

// CronUsageSummary holds token usage from a cron agent turn.
type CronUsageSummary struct {
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// CronRunTelemetry holds telemetry data from a cron job run.
type CronRunTelemetry struct {
	Model    string            `json:"model,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Usage    *CronUsageSummary `json:"usage,omitempty"`
}

// CronRunOutcome is the structured execution result.
type CronRunOutcome struct {
	Status     CronRunStatus `json:"status"`
	Error      string        `json:"error,omitempty"`
	ErrorKind  string        `json:"errorKind,omitempty"` // "delivery-target"
	Summary    string        `json:"summary,omitempty"`
	SessionID  string        `json:"sessionId,omitempty"`
	SessionKey string        `json:"sessionKey,omitempty"`
}

// CronJobFull is the full job definition matching TypeScript CronJob.
// Extends StoreJob with additional fields from types-shared.ts CronJobBase.
type CronJobFull struct {
	ID             string            `json:"id"`
	AgentID        string            `json:"agentId,omitempty"`
	SessionKey     string            `json:"sessionKey,omitempty"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Enabled        bool              `json:"enabled"`
	DeleteAfterRun bool              `json:"deleteAfterRun,omitempty"`
	CreatedAtMs    int64             `json:"createdAtMs,omitempty"`
	UpdatedAtMs    int64             `json:"updatedAtMs,omitempty"`
	Schedule       StoreSchedule     `json:"schedule"`
	SessionTarget  CronSessionTarget `json:"sessionTarget,omitempty"`
	WakeMode       CronWakeMode      `json:"wakeMode,omitempty"`
	Payload        StorePayload      `json:"payload"`
	Delivery       *CronDeliveryFull `json:"delivery,omitempty"`
	FailureAlert   *CronFailureAlert `json:"failureAlert,omitempty"`
	State          JobState          `json:"state"`
}

// InferDeliveryStatus infers the delivery status from execution outcome flags.
func InferDeliveryStatus(outcomeStatus CronRunStatus, deliveryAttempted, delivered, deliveryRequested bool) CronDeliveryStatus {
	if outcomeStatus != RunStatusOk {
		return DeliveryStatusUnknown
	}
	if deliveryAttempted && delivered {
		return DeliveryStatusDelivered
	}
	if deliveryAttempted && !delivered {
		return DeliveryStatusNotDelivered
	}
	if !deliveryAttempted && !delivered && deliveryRequested {
		return DeliveryStatusUnknown
	}
	return DeliveryStatusNotRequested
}
