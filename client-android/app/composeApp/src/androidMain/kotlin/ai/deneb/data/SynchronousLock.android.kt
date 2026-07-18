package ai.deneb.data

internal actual class SynchronousLock {
    private val monitor = Any()

    actual fun <T> withLock(action: () -> T): T = synchronized(monitor, action)
}
