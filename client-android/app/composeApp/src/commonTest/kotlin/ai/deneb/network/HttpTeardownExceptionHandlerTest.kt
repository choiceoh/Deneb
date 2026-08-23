package ai.deneb.network

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.launch
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class HttpTeardownExceptionHandlerTest {

    @Test
    fun matchesTeardownSignatureAnywhereInCauseChain() {
        val root = IllegalStateException("Unbalanced enter/exit")
        assertTrue(isHttpTeardownCrash(root))
        // The shape the crash reporter records: the platform ISE at the bottom of
        // a chain of cancel-cascade wrappers (CompletionHandlerException is
        // internal API, but the classifier only walks causes, not wrapper types).
        val wrapped = RuntimeException(
            "Exception in completion handler",
            RuntimeException("parent cancelled child", root),
        )
        assertTrue(isHttpTeardownCrash(wrapped))
    }

    @Test
    fun ignoresUnrelatedExceptions() {
        assertFalse(isHttpTeardownCrash(IllegalStateException("some other state error")))
        // Message alone is not enough — the type must match too.
        assertFalse(isHttpTeardownCrash(RuntimeException("Unbalanced enter/exit")))
        assertFalse(
            isHttpTeardownCrash(RuntimeException("x", IllegalArgumentException("Unbalanced enter/exit"))),
        )
    }

    @Test
    fun matchesNetworkOnMainThreadVariantAnywhereInCauseChain() {
        // The build-785 shape: same synchronous close, but the reader was between
        // reads so the drain ran on the main thread and StrictMode vetoed it.
        // Matched by class name because commonMain cannot reference the Android type.
        assertTrue(isHttpTeardownCrash(NetworkOnMainThreadException()))
        assertTrue(
            isHttpTeardownCrash(
                RuntimeException(
                    "Exception in completion handler",
                    RuntimeException("parent cancelled child", NetworkOnMainThreadException()),
                ),
            ),
        )
        // Name match is exact — a differently-named network error still crashes.
        assertFalse(isHttpTeardownCrash(NetworkOnSomeOtherThreadException()))
    }

    /** Stands in for `android.os.NetworkOnMainThreadException`, matched by simple name. */
    private class NetworkOnMainThreadException : RuntimeException("network on main thread")

    private class NetworkOnSomeOtherThreadException : RuntimeException("unrelated")

    @Test
    fun handlerContainsTeardownRaisedByCancel() {
        // Reproduces the crash mechanism: a completion handler that throws while
        // the job is cancelled. kotlinx.coroutines wraps the throw and delivers it
        // to the coroutine context's CoroutineExceptionHandler — never out of
        // cancel() itself (why a runCatching around cancel() couldn't fix build
        // 611). With the handler installed, cancel() completes and nothing reaches
        // the uncaught handler.
        val scope = CoroutineScope(Dispatchers.Unconfined + httpTeardownTolerantHandler("test"))
        val job = scope.launch { awaitCancellation() }
        job.invokeOnCompletion { cause ->
            if (cause is CancellationException) throw IllegalStateException("Unbalanced enter/exit")
        }
        job.cancel()
        assertTrue(job.isCancelled)
    }
}
