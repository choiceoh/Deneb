package ai.deneb.sensing

// Web has no location sensing surface — location is an Android-only (foss) signal.
actual suspend fun readCurrentLocation(): String? = null
