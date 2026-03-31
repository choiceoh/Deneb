package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
)

// HookManager groups the hooks registry, HTTP webhook handler, and the
// scheduled-task (cron) subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type HookManager struct {
	hooks         *hooks.Registry
	hooksHTTP     *HooksHTTPHandler
	internalHooks *hooks.InternalRegistry
	cron          *cron.Scheduler
	cronRunLog    *cron.PersistentRunLog
	cronService   *cron.Service
}
