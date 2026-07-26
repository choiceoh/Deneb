package ai.deneb.sensing

/**
 * Recent-call sensing, mirroring [readWorkUsageDigest].
 *
 * Who the operator spoke to and for how long is 인물/미팅 context this assistant
 * already reasons about — it keeps 인물 pages, harvests meetings, and matches
 * counterparties — but the call log was the one channel it could not see after
 * the Termux retirement. Android reads `CallLog.Calls` (needs the user-granted
 * READ_CALL_LOG); every other target returns null (no-op).
 *
 * Returns null when there is nothing worth forwarding: permission not granted,
 * no calls in the window, or a non-Android target. Coarse by design — the other
 * party, direction, rounded duration, and time, newest first, capped. Never the
 * full history, and never call *content* (there is none to read anyway).
 *
 * The caller forwards the digest as a `calllog_update` event; the gateway caches
 * it for `phone_read("calllog")` and raises no proactive alert from it, because
 * a call that already ended is context for the next question, not a reason to
 * interrupt.
 */
expect fun readRecentCallDigest(): String?
