package ai.deneb.deneb

import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebView
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import java.util.ArrayDeque

internal const val BROWSER_TRANSLATE_BRIDGE_NAME = "DenebTranslateBridge"

private const val MAX_TRANSLATE_IN_FLIGHT = 8
private const val MAX_TRANSLATE_QUEUED = 128
private const val MAX_REPORTED_TRANSLATION_NODES = 100_000

private val browserTranslateJson = Json { ignoreUnknownKeys = true }
private val browserTranslateStringListSerializer = ListSerializer(String.serializer())

private data class BrowserTranslationWork(
    val requestId: String,
    val segments: List<String>,
    val generation: Long,
)

/**
 * Bounded JS-to-native translation scheduler. Eight requests may execute at
 * once; later batches wait instead of being rejected and permanently skipped.
 */
internal class BrowserTranslateBridge(
    private val scope: CoroutineScope,
    private val translateSegments: TranslateFn,
    private val state: DenebWebViewState,
    private val webView: () -> WebView?,
    private val translationEnabled: () -> Boolean,
) {
    private val lock = Any()
    private val queued = ArrayDeque<BrowserTranslationWork>()
    private var active = 0
    private var generation = 0L
    private var generationScope = newGenerationScope()

    @JavascriptInterface
    fun onEnable(count: Int) {
        if (!translationEnabled()) return
        val total = count.coerceIn(0, MAX_REPORTED_TRANSLATION_NODES)
        Handler(Looper.getMainLooper()).post {
            if (translationEnabled()) state.updateTranslationProgress(total, applied = 0, pending = 0, failed = 0)
        }
    }

    @JavascriptInterface
    fun onProgress(total: Int, applied: Int, pending: Int, failed: Int) {
        if (!translationEnabled()) return
        val cleanTotal = total.coerceIn(0, MAX_REPORTED_TRANSLATION_NODES)
        Handler(Looper.getMainLooper()).post {
            if (translationEnabled()) state.updateTranslationProgress(cleanTotal, applied, pending, failed)
        }
    }

    @JavascriptInterface
    fun translate(requestId: String, segmentsJson: String) {
        if (!translationEnabled() || !isBrowserTranslateEnvelopeWithinLimits(requestId, segmentsJson.length)) {
            reject(requestId, retryable = false)
            return
        }
        val segments = decodeStringList(segmentsJson)
        if (!areBrowserTranslateSegmentsWithinLimits(segments)) {
            reject(requestId, retryable = false)
            return
        }
        val ready = synchronized(lock) {
            if (queued.size >= MAX_TRANSLATE_QUEUED) {
                null
            } else {
                queued.addLast(BrowserTranslationWork(requestId, segments, generation))
                drainReadyLocked()
            }
        }
        if (ready == null) {
            reject(requestId, retryable = true)
            return
        }
        launchAll(ready)
    }

    /** Drops the old document's queued work and cancels its active RPCs. A new
     * page gets a fresh scheduler generation instead of waiting behind it. */
    fun cancelForNavigation() {
        val expired = synchronized(lock) {
            generation++
            queued.clear()
            active = 0
            generationScope.also {
                generationScope = newGenerationScope()
            }
        }
        expired.cancel()
    }

    private fun drainReadyLocked(): List<BrowserTranslationWork> {
        val ready = ArrayList<BrowserTranslationWork>(MAX_TRANSLATE_IN_FLIGHT)
        while (active < MAX_TRANSLATE_IN_FLIGHT && queued.isNotEmpty()) {
            active++
            ready += queued.removeFirst()
        }
        return ready
    }

    private fun launchAll(ready: List<BrowserTranslationWork>) {
        ready.forEach { work ->
            val executionScope = synchronized(lock) {
                generationScope.takeIf { work.generation == generation }
            } ?: return@forEach
            executionScope.launch {
                try {
                    if (!translationEnabled()) {
                        reject(work.requestId, retryable = false)
                        return@launch
                    }
                    markStarted(work.requestId)
                    val translated = try {
                        translateSegments(work.segments, "ko")
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (_: Exception) {
                        null
                    }
                    if (translated == null || translated.size != work.segments.size) {
                        reject(work.requestId, retryable = false)
                        return@launch
                    }
                    val requestLiteral = jsStringLiteral(work.requestId)
                    val payloadLiteral = jsStringLiteral(encodeStringList(translated))
                    withContext(Dispatchers.Main) {
                        webView()?.evaluateJavascript(
                            "window.DenebTranslate&&window.DenebTranslate.applyBatch($requestLiteral,$payloadLiteral);",
                            null,
                        )
                    }
                } finally {
                    val next = synchronized(lock) {
                        if (work.generation == generation) {
                            active = (active - 1).coerceAtLeast(0)
                            drainReadyLocked()
                        } else {
                            emptyList()
                        }
                    }
                    launchAll(next)
                }
            }
        }
    }

    private fun newGenerationScope(): CoroutineScope = CoroutineScope(
        scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]),
    )

    private suspend fun markStarted(requestId: String) {
        val requestLiteral = jsStringLiteral(requestId)
        withContext(Dispatchers.Main.immediate) {
            webView()?.evaluateJavascript(
                "window.DenebTranslate&&window.DenebTranslate.beginBatch&&" +
                    "window.DenebTranslate.beginBatch($requestLiteral);",
                null,
            )
        }
    }

    private fun reject(requestId: String, retryable: Boolean) {
        if (!isBrowserTranslateEnvelopeWithinLimits(requestId, 0)) return
        val requestLiteral = jsStringLiteral(requestId)
        Handler(Looper.getMainLooper()).post {
            webView()?.evaluateJavascript(
                "window.DenebTranslate&&window.DenebTranslate.rejectBatch&&" +
                    "window.DenebTranslate.rejectBatch($requestLiteral,$retryable);",
                null,
            )
        }
    }
}

private fun decodeStringList(json: String): List<String> = runCatching {
    browserTranslateJson.decodeFromString(browserTranslateStringListSerializer, json)
}.getOrDefault(emptyList())

private fun encodeStringList(values: List<String>): String = browserTranslateJson.encodeToString(browserTranslateStringListSerializer, values)

/** JSON string encoding makes values safe inside evaluateJavascript source. */
private fun jsStringLiteral(value: String): String = browserTranslateJson.encodeToString(String.serializer(), value)
