package ai.deneb.network

import ai.deneb.DenebLog
import kotlinx.coroutines.CoroutineExceptionHandler

/**
 * Containment for the Android platform-okhttp stream-teardown crash (crash
 * reporter builds 609–614 and 785; #3337 → #3340 fixed it for TaskScheduler only).
 *
 * Cancelling a coroutine that owns an in-flight ktor(Android engine) request
 * runs ktor's cleanup handler synchronously on the cancelling thread: ktor's
 * RawSourceChannel registers `invokeOnCompletion { closeSource() }`, and that
 * close drains the HttpURLConnection response stream on whichever thread called
 * `cancel()`. Draining is a socket read, so cancelling from the main thread
 * fails one of two ways — the reader thread is parked in the same stream's
 * AsyncTimeout and the platform okhttp (com.android.okhttp.okio.AsyncTimeout) is
 * not safe against the concurrent enter (`IllegalStateException: Unbalanced
 * enter/exit`), or the read happens on the main thread outright and StrictMode
 * raises `NetworkOnMainThreadException`. See [isHttpTeardownCrash].
 * kotlinx.coroutines wraps either in CompletionHandlerException and delivers it
 * via handleCoroutineException on the CANCELLED coroutine's context; it is
 * never thrown out of `cancel()` itself (why a runCatching around cancel()
 * cannot catch it — proven live by build 611). The only interception point is
 * a CoroutineExceptionHandler in the context of the coroutine being cancelled,
 * so install this in every scope/launch whose job can be cancel()ed while a
 * gateway request may be streaming.
 *
 * Matching teardowns are swallowed — the job is already cancelled and the
 * half-closed connection is abandoned to the platform. Everything else is
 * rethrown: rethrowing the SAME instance from a CEH makes kotlinx.coroutines
 * (handleCoroutineException → handlerException) fall through to the exact
 * no-handler uncaught path, so real bugs keep crashing loudly.
 *
 * TaskScheduler deliberately keeps its broader swallow-everything handler
 * (subscription-loop resilience — see schedulerExceptionHandler there); this
 * one is for scopes where non-teardown exceptions must still crash.
 */
fun httpTeardownTolerantHandler(tag: String): CoroutineExceptionHandler = CoroutineExceptionHandler { _, exception ->
    if (!isHttpTeardownCrash(exception)) throw exception
    DenebLog.warn(tag, "ignored http stream teardown on cancel: ${exception.message}")
}

/**
 * The teardown has two signatures anywhere in the cause chain, both produced by
 * the same synchronous close described above — which one you get depends on what
 * the reader thread was doing when the close raced it:
 *
 * 1. [IllegalStateException] carrying okio's AsyncTimeout concurrency message —
 *    the reader was parked inside the timeout (typical for an idle SSE stream).
 *    Message-matched because the throwing class (com.android.okhttp / okio
 *    AsyncTimeout) is platform-internal and unavailable to reference; the
 *    type+message pair is unique to this race.
 * 2. `android.os.NetworkOnMainThreadException` — the reader was between reads, so
 *    the close drained the socket itself, on the main thread, which StrictMode
 *    then vetoes (typical for a short RPC cancelled mid-body; build 785). Matched
 *    by class name: commonMain cannot reference an Android platform type, and the
 *    name is specific enough that no other throwable shares it.
 *
 * Matching by class name (rather than message) keeps this narrow: a
 * RuntimeException that merely quotes the AsyncTimeout message is not a teardown
 * and must still crash.
 */
internal fun isHttpTeardownCrash(exception: Throwable): Boolean {
    var current: Throwable? = exception
    var depth = 0
    while (current != null && depth < 16) {
        if (current is IllegalStateException && current.message?.contains("Unbalanced enter/exit") == true) {
            return true
        }
        if (current::class.simpleName == "NetworkOnMainThreadException") {
            return true
        }
        current = current.cause
        depth++
    }
    return false
}
