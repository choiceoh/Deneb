package ai.deneb.deneb

import java.io.File
import kotlin.test.Test
import kotlin.test.assertContains
import kotlin.test.assertFalse

/**
 * The height reporter and `denebHtmlFrameHeight` are one mechanism: the frame may
 * only shrink because the page reports its own CONTENT height. Reporting
 * `documentElement.scrollHeight` instead — `max(content, viewport)` — means a
 * frame that has overshot hears its own height back and can never learn it was
 * wrong, which is exactly how a card ended up with screens of blank under it.
 *
 * Both renderers inject their own copy of that script, so this asserts on both.
 */
class DenebHtmlPreludeContractTest {
    private val nativePrelude = code(
        repoFile("client-android/app/composeApp/src/androidMain/kotlin/ai/deneb/ui/dynamicui/DenebHtmlView.android.kt")
            .readText(),
    )
    private val desktopPrelude = code(repoFile("andromeda/src/components/denebHtmlSandbox.ts").readText())

    @Test
    fun neitherRendererReportsTheViewportClampedHeight() {
        for ((name, source) in listOf("native" to nativePrelude, "desktop" to desktopPrelude)) {
            assertFalse(
                source.contains("documentElement.scrollHeight"),
                "$name reports max(content, viewport); the frame could never shrink",
            )
        }
    }

    @Test
    fun bothRenderersMeasureTheBodyBoxWithItsMargins() {
        for ((name, source) in listOf("native" to nativePrelude, "desktop" to desktopPrelude)) {
            assertContains(source, "b.getBoundingClientRect().height", message = "$name lost the body measurement")
            assertContains(source, "parseFloat(s.marginTop)", message = "$name lost the body margins")
            assertContains(source, "parseFloat(s.marginBottom)", message = "$name lost the body margins")
        }
    }

    @Test
    fun bothRenderersRemeasureOnceFontsLand() {
        // Fonts settle after `load` and change every line box. Without this report
        // the pre-swap (taller) measurement is the last one either side ever hears.
        for ((name, source) in listOf("native" to nativePrelude, "desktop" to desktopPrelude)) {
            assertContains(source, "document.fonts.ready.then(r)", message = "$name never remeasures after the font swap")
        }
    }

    /**
     * Comments out. Both files explain in prose why `documentElement.scrollHeight`
     * is banned here, and a test that greps the whole file would fail on its own
     * explanation — then get "fixed" by deleting the explanation.
     */
    private fun code(source: String): String = source.lineSequence()
        .filterNot {
            val t = it.trimStart()
            t.startsWith("//") || t.startsWith("*") || t.startsWith("/*")
        }
        .joinToString("\n")

    private fun repoFile(relative: String): File {
        var dir: File? = File(".").absoluteFile
        while (dir != null) {
            val candidate = File(dir, relative)
            if (candidate.isFile) return candidate
            dir = dir.parentFile
        }
        error("missing source file: $relative")
    }
}
