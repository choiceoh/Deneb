// Package observebind wires the concrete observe tool so toolbind's root
// package does not import runtimeops + observe leaf deps directly.
package observebind

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/runtimeops/observeops"
)

// ToolObserve binds the concrete observeops observe tool.
func ToolObserve(lc *observe.LogCapture, alog *agentlog.Writer, wf *workfeed.Store, vllmBases func() []string) toolport.ToolFunc {
	return observeops.ToolObserve(lc, alog, wf, vllmBases)
}
