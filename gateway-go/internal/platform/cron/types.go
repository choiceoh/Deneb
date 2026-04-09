// types.go — Cron type definitions.
package cron

// CronSessionTarget specifies which session to run the job in.
type CronSessionTarget string

const (
	SessionTargetMain     CronSessionTarget = "main"
	SessionTargetIsolated CronSessionTarget = "isolated"
	SessionTargetCurrent  CronSessionTarget = "current"
	SessionTargetSubagent CronSessionTarget = "subagent" // clone main session transcript
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
