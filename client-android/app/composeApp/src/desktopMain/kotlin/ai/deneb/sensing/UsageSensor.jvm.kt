package ai.deneb.sensing

// Desktop has no app-usage sensing surface — the work launcher's usage rhythm is an
// Android-only signal.
actual fun readWorkUsageDigest(): String? = null
