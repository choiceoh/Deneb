// Package timeouts provides centralized timeout constants for the gateway.
// Single-user deployment: values are opinionated defaults (not configurable).
package timeouts

import "time"

const (
	// RPCDispatch bounds how long a single RPC handler can run before
	// being canceled. Prevents a stuck handler from blocking the message loop.
	RPCDispatch = 30 * time.Second

	// ProcessExec is the default timeout for process execution.
	ProcessExec = 60 * time.Second

	// ProviderHTTP is the default timeout for provider HTTP calls.
	ProviderHTTP = 30 * time.Second

	// GracefulShutdown is the timeout for graceful server shutdown.
	GracefulShutdown = 5 * time.Second
)
