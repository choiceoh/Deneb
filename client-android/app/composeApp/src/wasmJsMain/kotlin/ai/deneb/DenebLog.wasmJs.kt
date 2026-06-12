package ai.deneb

// println maps to the browser console on wasmJs; the level prefix keeps it
// filterable in devtools.
actual object DenebLog {
    actual fun debug(tag: String, message: String) {
        println("DEBUG [$tag] $message")
    }

    actual fun warn(tag: String, message: String) {
        println("WARN  [$tag] $message")
    }

    actual fun error(tag: String, message: String, throwable: Throwable?) {
        println("ERROR [$tag] $message")
        throwable?.let { println(it.stackTraceToString()) }
    }
}
