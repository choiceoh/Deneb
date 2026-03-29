// Package system provides RPC method handlers for the system domain:
// identity, monitoring, doctor, maintenance, update, usage, and logs.
//
// It exposes *Methods functions that return handler maps for bulk-registration
// on the rpc.Dispatcher.
package system

// BroadcastFunc is the signature for broadcasting events to connected clients.
type BroadcastFunc func(event string, payload any) (int, []error)
