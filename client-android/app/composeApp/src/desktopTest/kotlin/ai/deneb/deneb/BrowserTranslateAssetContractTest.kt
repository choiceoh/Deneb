package ai.deneb.deneb

import java.io.File
import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserTranslateAssetContractTest {
    @Test
    fun producerPayloadAndLongTextStayInsideNativeGatewayBounds() {
        val source = sourceFile("src/androidMain/assets/deneb-translate.js").readText()
        val segmentChars = source.jsInt("MAX_SEGMENT_PAYLOAD_CHARS")
        val batchChars = source.jsInt("MAX_BATCH_PAYLOAD_CHARS")

        assertTrue(segmentChars <= MAX_BROWSER_TRANSLATE_SEGMENT_CHARS)
        assertTrue(batchChars <= MAX_BROWSER_TRANSLATE_TOTAL_CHARS)
        assertContains(source, "function splitLongText(text)")
        assertContains(source, "function expandShipUnit(unit)")
        assertContains(source, "MAX_BATCH_JSON_CHARS")
        assertContains(source, "joiners: split.joiners")
        assertFalse(source.contains("accumulator.values.join(' ')"))
    }

    @Test
    fun nativeBridgeQueuesOverflowBehindTheExecutionLimit() {
        val nativeSource = sourceFile(
            "src/androidMain/kotlin/ai/deneb/deneb/BrowserTranslateBridge.android.kt",
        ).readText()
        val jsSource = sourceFile("src/androidMain/assets/deneb-translate.js").readText()

        assertTrue(nativeSource.kotlinInt("MAX_TRANSLATE_QUEUED") > nativeSource.kotlinInt("MAX_TRANSLATE_IN_FLIGHT"))
        assertContains(nativeSource, "queued.addLast(BrowserTranslationWork")
        assertContains(nativeSource, "drainReadyLocked()")
        assertContains(nativeSource, "markStarted(work.requestId)")
        assertContains(nativeSource, "fun cancelForNavigation()")
        assertContains(nativeSource, "queued.clear()")
        assertContains(nativeSource, "acceptingRequests = false")
        assertContains(nativeSource, "suspendDocumentForNavigation()")
        assertContains(nativeSource, "fun resumeForDocument()")
        assertContains(jsSource, "function beginBatch(rid)")
        assertContains(jsSource, "function cancelBatch(rid)")
        assertContains(jsSource, "function suspendForNavigation()")
        assertTrue(jsSource.jsInt("NATIVE_BATCH_TIMEOUT_MS").toLong() > DenebGatewayClient.REQUEST_TIMEOUT_MS)

        val navigationCancelBody = jsSource.substringAfter("function cancelBatch(rid)").substringBefore("function replace")
        assertFalse(navigationCancelBody.contains("failUnits("))
        assertFalse(navigationCancelBody.contains("reportProgress("))
        assertFalse(navigationCancelBody.contains("dispatchPrioritized("))

        val webViewSource = sourceFile(
            "src/androidMain/kotlin/ai/deneb/deneb/DenebWebView.android.kt",
        ).readText()
        assertContains(webViewSource, "holder.translateBridge?.cancelForNavigation()")
        assertContains(webViewSource, "holder.translateBridge?.resumeForDocument()")
        assertContains(webViewSource, "val target = browserWebViewCommandUrl(")
        assertFalse(webViewSource.contains("DETACHED_RESTORE_GUARD_MS"))
    }

    @Test
    fun identityTranslationsCountAsCompletedInsteadOfFailed() {
        val source = sourceFile("src/androidMain/assets/deneb-translate.js").readText()
        val applyBody = source.substringAfter("function applyTranslationToTid").substringBefore("function textLength")

        assertContains(applyBody, "rememberTranslation(rec.original, translated)")
        assertFalse(applyBody.contains("translated === rec.original"))
    }

    @Test
    fun transientNativeFailuresRetryWithBoundedBackoff() {
        val nativeSource = sourceFile(
            "src/androidMain/kotlin/ai/deneb/deneb/BrowserTranslateBridge.android.kt",
        ).readText()
        val jsSource = sourceFile("src/androidMain/assets/deneb-translate.js").readText()

        val nativeResultBody = nativeSource.substringAfter("if (translated == null)").substringBefore("val requestLiteral")
        assertContains(nativeResultBody, "reject(work.requestId, retryable = true)")
        assertContains(nativeResultBody, "reject(work.requestId, retryable = false)")

        assertTrue(jsSource.jsInt("MAX_TRANSIENT_RETRIES") in 1..6)
        assertContains(jsSource, "retryAttempts[tid] = attempt")
        assertContains(jsSource, "RETRY_BASE_DELAY_MS * Math.pow(2, attempt - 1)")
        assertContains(jsSource, "if (inFlight[tids[i]] || failed[tids[i]] || isApplied(rec)) continue")
        val disableBody = jsSource.substringAfter("// Restore originals.").substringBefore("function onLocationChange")
        assertContains(disableBody, "delete failed[tid]")
        assertContains(disableBody, "delete retryAttempts[tid]")
    }

    private fun sourceFile(relative: String): File = sequenceOf(
        File(relative),
        File("composeApp/$relative"),
        File("client-android/app/composeApp/$relative"),
    ).firstOrNull(File::isFile) ?: error("missing source file: $relative")

    private fun String.jsInt(name: String): Int = Regex("var\\s+$name\\s*=\\s*(\\d+)").find(this)?.groupValues?.get(1)?.toInt()
        ?: error("missing JS constant: $name")

    private fun String.kotlinInt(name: String): Int = Regex("const\\s+val\\s+$name\\s*=\\s*(\\d+)").find(this)?.groupValues?.get(1)?.toInt()
        ?: error("missing Kotlin constant: $name")
}
