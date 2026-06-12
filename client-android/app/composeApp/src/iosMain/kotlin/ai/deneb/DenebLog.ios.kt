package ai.deneb

// println reaches the Xcode console / os stdout on iOS; the level prefix
// keeps it filterable. NSLog is avoided to keep this file dependency-free.
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
