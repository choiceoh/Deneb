// Package process contains RPC handlers for ACP (Agent Communication Protocol)
// and advanced cron operations. These were migrated from the flat rpc package
// into a domain-based subpackage.
package process

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// ---------------------------------------------------------------------------
// ACP (Agent Communication Protocol)
