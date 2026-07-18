package ai.deneb.data

/**
 * Small cross-platform lock for state that must be read from non-suspending UI
 * code as well as coroutine loaders. Keep actions short and never suspend while
 * holding this lock.
 */
internal expect class SynchronousLock() {
    fun <T> withLock(action: () -> T): T
}
