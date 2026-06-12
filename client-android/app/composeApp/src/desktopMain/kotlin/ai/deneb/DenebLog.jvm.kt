package ai.deneb

// stderr so the native-app harness log (and any service wrapper) separates
// diagnostics from stdout; the level prefix makes `logs-grep`-style filtering
// possible without structured logging machinery.
actual object DenebLog {
    actual fun debug(tag: String, message: String) {
        System.err.println("DEBUG [$tag] $message")
    }

    actual fun warn(tag: String, message: String) {
        System.err.println("WARN  [$tag] $message")
    }

    actual fun error(tag: String, message: String, throwable: Throwable?) {
        System.err.println("ERROR [$tag] $message")
        throwable?.printStackTrace()
    }
}
