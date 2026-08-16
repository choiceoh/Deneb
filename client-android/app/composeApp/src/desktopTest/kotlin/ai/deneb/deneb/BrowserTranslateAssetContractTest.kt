package ai.deneb.deneb

import java.io.File
import kotlin.test.Test
import kotlin.test.assertContains
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
    }

    @Test
    fun nativeBridgeQueuesOverflowBehindTheExecutionLimit() {
        val source = sourceFile(
            "src/androidMain/kotlin/ai/deneb/deneb/BrowserTranslateBridge.android.kt",
        ).readText()

        assertTrue(source.kotlinInt("MAX_TRANSLATE_QUEUED") > source.kotlinInt("MAX_TRANSLATE_IN_FLIGHT"))
        assertContains(source, "queued.addLast(work)")
        assertContains(source, "drainReadyLocked()")
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
