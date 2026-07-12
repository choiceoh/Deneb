package ai.deneb.network

import ai.deneb.DenebLog
import kotlinx.coroutines.CoroutineExceptionHandler

/**
 * Containment for the Android platform-okhttp stream-teardown crash (crash
 * reporter builds 609–614; #3337 → #3340 fixed it for TaskScheduler only).
 *
 * Cancelling a coroutine that owns an in-flight ktor(Android engine) request
 * runs ktor's cleanup handler synchronously on the cancelling thread. That
 * close drains the HttpURLConnection response stream while the reader thread
 * is blocked inside the same stream's AsyncTimeout, and the platform okhttp
 * (com.android.okhttp.okio.AsyncTimeout) is not safe against the concurrent
 * enter — it throws `IllegalStateException: Unbalanced enter/exit`.
 * kotlinx.coroutines wraps that in CompletionHandlerException and delivers it
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
 * The teardown signature: an [IllegalStateException] with the AsyncTimeout
 * concurrency message anywhere in the cause chain. Message-matched because the
 * throwing class (com.android.okhttp / okio AsyncTimeout) is platform-internal
 * and unavailable to reference; the type+message pair is unique to this race.
 */
internal fun isHttpTeardownCrash(exception: Throwable): Boolean {
    var current: Throwable? = exception
    var depth = 0
    while (current != null && depth < 16) {
        if (current is IllegalStateException && current.message?.contains("Unbalanced enter/exit") == true) {
            return true
        }
        current = current.cause
        depth++
    }
    return false
}
