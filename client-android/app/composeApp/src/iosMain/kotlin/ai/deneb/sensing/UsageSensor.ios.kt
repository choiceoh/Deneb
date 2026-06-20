package ai.deneb.sensing

// iOS does not expose cross-app usage statistics to third-party apps — no digest.
actual fun readWorkUsageDigest(): String? = null
