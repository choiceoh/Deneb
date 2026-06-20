package ai.deneb.sensing

/**
 * On-demand location read, mirroring [readWorkUsageDigest]. Android resolves the
 * FusedLocationProvider (needs a user-granted FINE/COARSE location permission) and
 * returns a compact JSON fix; every other target returns null (no-op).
 *
 * The caller forwards a non-null result — throttled — to the gateway as a
 * `location_update` event, which caches it for `phone_read("location")` (no judgment
 * turn). Returns null when permission is not granted or no fix is available, so the
 * gateway keeps its last cached value / falls back to a live read.
 */
expect suspend fun readCurrentLocation(): String?
