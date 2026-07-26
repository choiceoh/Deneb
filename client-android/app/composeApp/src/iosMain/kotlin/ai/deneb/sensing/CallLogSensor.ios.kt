package ai.deneb.sensing

// Call-log sensing is an Android-only signal — every other target has no phone
// call history to read.
actual fun readRecentCallDigest(): String? = null
