// Package timeouts provides centralized timeout constants for the gateway.
// Single-user deployment: values are opinionated defaults (not configurable).
package timeouts

import "time"

const (
	// RPCDispatch bounds how long a single RPC handler can run before
	// being canceled. Prevents a stuck handler from blocking the message loop.
	RPCDispatch = 30 * time.Second

	// BridgeForward is the default timeout for bridge RPC forwarding
	// when the caller's context has no deadline.
	BridgeForward = 60 * time.Second

	// BridgeReconnectMax is the maximum backoff between bridge reconnect attempts.
	BridgeReconnectMax = 30 * time.Second

	// BridgeDial is the timeout for a single bridge dial attempt.
	BridgeDial = 5 * time.Second

	// ProcessExec is the default timeout for process execution.
	ProcessExec = 60 * time.Second

	// ProviderHTTP is the default timeout for provider HTTP calls.
	ProviderHTTP = 30 * time.Second

	// SocketWait is how long to wait for a Unix socket to appear.
	SocketWait = 10 * time.Second

	// GracefulShutdown is the timeout for graceful server shutdown.
	GracefulShutdown = 5 * time.Second
)
