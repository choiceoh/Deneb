package ai.deneb.data

import platform.Foundation.NSLock

internal actual class SynchronousLock {
    private val lock = NSLock()

    actual fun <T> withLock(action: () -> T): T {
        lock.lock()
        return try {
            action()
        } finally {
            lock.unlock()
        }
    }
}
