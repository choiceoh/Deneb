package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
)

// HookManager groups the internal hooks registry and
// the scheduled-task (cron) subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type HookManager struct {
	internalHooks *hooks.InternalRegistry
	cronRunLog    *cron.PersistentRunLog
	cronService   *cron.Service
}
