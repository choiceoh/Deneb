package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
)

// HookManager groups the internal hooks registry, HTTP webhook handler, and
// the scheduled-task (cron) subsystems.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type HookManager struct {
	hooksHTTP     *HooksHTTPHandler
	internalHooks *hooks.InternalRegistry
	cronRunLog    *cron.PersistentRunLog
	cronService   *cron.Service
}
