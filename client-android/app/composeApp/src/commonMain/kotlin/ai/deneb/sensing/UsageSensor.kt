package ai.deneb.sensing

/**
 * App-usage-rhythm sensing for the work launcher (Phase 0, "다 읽되 다 보여주지
 * 않는다"). Reads the phone's recent app usage on-device, compresses it to a
 * compact work-rhythm digest, and the caller forwards it — throttled — to the
 * gateway's event-ingest, which judges it with default-silence guidance so only a
 * genuine work signal ever surfaces. Android resolves UsageStatsManager (needs the
 * user-granted Usage access); every other target returns null (no-op).
 *
 * Returns null when there's nothing worth forwarding: permission not granted, no
 * significant foreground time, or a non-Android target. Coarse by design — app
 * label + rounded minutes, top apps only — never a detailed timeline. Per-app
 * switches are NEVER forwarded individually (that would be pure noise); only this
 * windowed digest leaves the device.
 */
expect fun readWorkUsageDigest(): String?
