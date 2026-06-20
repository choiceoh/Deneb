package ai.deneb.sensing

// Location sensing is an Android-only (foss) signal; no iOS surface.
actual suspend fun readCurrentLocation(): String? = null
