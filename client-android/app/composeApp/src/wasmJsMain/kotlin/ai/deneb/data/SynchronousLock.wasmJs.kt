package ai.deneb.data

/** wasm runs this cache on one event-loop thread. */
internal actual class SynchronousLock {
    actual fun <T> withLock(action: () -> T): T = action()
}
