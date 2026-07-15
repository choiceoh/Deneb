package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
)

// HookManager groups the scheduled-task (cron) subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type HookManager struct {
	cronRunLog  *platbind.PersistentRunLog
	cronService *platbind.CronService
}
