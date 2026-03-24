package protocol

const (
	// MaxPayloadBytes is the maximum size of an authenticated message (25 MB).
	MaxPayloadBytes = 25 * 1024 * 1024

	// MaxBufferedBytes is the per-connection send buffer limit (50 MB).
	MaxBufferedBytes = 50 * 1024 * 1024

	// MaxPreAuthPayloadBytes is the maximum size of a pre-handshake message (64 KB).
	MaxPreAuthPayloadBytes = 64 * 1024

	// HandshakeTimeoutMs is the default handshake timeout in milliseconds.
	HandshakeTimeoutMs = 3_000

	// TickIntervalMs is the server heartbeat interval in milliseconds.
	TickIntervalMs = 30_000

	// HealthRefreshIntervalMs is the health snapshot refresh interval in milliseconds.
	HealthRefreshIntervalMs = 60_000

	// DedupeTTLMs is the idempotency window in milliseconds (5 minutes).
	DedupeTTLMs = 5 * 60_000

	// DedupeMax is the maximum number of dedupe entries before cleanup.
	DedupeMax = 1000
)
