package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
)

// initHooksFromConfig creates the internal hook registry.
// User-defined shell hooks (Registry) have been removed; only the programmatic
// InternalRegistry remains.
func (s *Server) initHooksFromConfig() {
	s.internalHooks = hooks.NewInternalRegistry(s.logger)
}
