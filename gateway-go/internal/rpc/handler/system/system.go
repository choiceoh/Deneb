// Package system provides RPC method handlers for the system domain:
// identity, monitoring, doctor, maintenance, update, usage, and logs.
//
// It exposes *Methods functions that return handler maps for bulk-registration
// on the rpc.Dispatcher.
package system

import "github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc
