// types.go — Full cron type definitions matching src/cron/types.ts + types-shared.ts.
package cron

// CronDeliveryMode defines how cron output is delivered.
// Telegram-only deployment: only "none" and "announce" are used.
type CronDeliveryMode string

const (
	DeliveryModeNone     CronDeliveryMode = "none"
	DeliveryModeAnnounce CronDeliveryMode = "announce"
)

// CronSessionTarget specifies which session to run the job in.
type CronSessionTarget string

const (
	SessionTargetMain     CronSessionTarget = "main"
	SessionTargetIsolated CronSessionTarget = "isolated"
	SessionTargetCurrent  CronSessionTarget = "current"
	SessionTargetSubagent CronSessionTarget = "subagent" // clone main session transcript
)

// CronWakeMode controls post-execution wake behavior.
type CronWakeMode string

const (
	WakeModeNextHeartbeat CronWakeMode = "next-heartbeat"
	WakeModeNow           CronWakeMode = "now"
)

// CronFailureAlert configures failure notification behavior.
type CronFailureAlert struct {
	After      int    `json:"after,omitempty"`
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	CooldownMs int64  `json:"cooldownMs,omitempty"`
	Mode       string `json:"mode,omitempty"` // "announce" (Telegram delivery)
	AccountID  string `json:"accountId,omitempty"`
}
