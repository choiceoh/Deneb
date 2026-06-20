package ai.deneb.sensing

// Geofencing is an Android-only (foss) feature.
actual suspend fun applyGeofences(geofences: List<DenebGeofence>): Boolean = false
