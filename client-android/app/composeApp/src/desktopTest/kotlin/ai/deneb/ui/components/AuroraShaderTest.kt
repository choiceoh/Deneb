package ai.deneb.ui.components

import org.jetbrains.skia.Bitmap
import org.jetbrains.skia.Paint
import org.jetbrains.skia.Rect
import org.jetbrains.skia.RuntimeEffect
import org.jetbrains.skia.RuntimeShaderBuilder
import org.jetbrains.skia.Surface
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * Renders the shared aurora shader (the exact source Android's RuntimeShader compiles
 * as AGSL) to a CPU raster surface and checks it paints a top-lit, bottom-faded aurora
 * with horizontal hue variation. This verifies the SkSL/AGSL source — the render
 * harness can't (its SOFTWARE skiko window silently no-ops runtime effects), and the
 * desktop runtime falls back to the slice draw, so this test is the source's guardrail.
 */
class AuroraShaderTest {

    private fun render(time: Float, w: Int = 160, h: Int = 320): Bitmap {
        val effect = RuntimeEffect.makeForShader(AURORA_SHADER_SRC) // throws on invalid SkSL
        val builder = RuntimeShaderBuilder(effect).apply {
            uniform("uResolution", w.toFloat(), h.toFloat())
            uniform("uTime", time)
            uniform("uIntensity", 1f)
        }
        val surface = Surface.makeRasterN32Premul(w, h)
        surface.canvas.clear(0x00000000)
        surface.canvas.drawRect(
            Rect.makeWH(w.toFloat(), h.toFloat()),
            Paint().apply { shader = builder.makeShader() },
        )
        return Bitmap().apply { allocN32Pixels(w, h) }.also {
            assertTrue(surface.readPixels(it, 0, 0), "readPixels failed")
        }
    }

    private fun alphaAt(bmp: Bitmap, x: Int, y: Int): Int = (bmp.getColor(x, y) ushr 24) and 0xFF

    @Test
    fun shaderRendersTopLitAuroraThatFadesDown() {
        val bmp = render(time = 0f) // t=0 -> blue start, deepest curtain
        val w = bmp.width
        val h = bmp.height
        val topAlpha = alphaAt(bmp, w / 2, 6)
        val bottomAlpha = alphaAt(bmp, w / 2, h - 6)
        assertTrue(topAlpha > 10, "top should be lit by the aurora (alpha=$topAlpha)")
        assertTrue(bottomAlpha < topAlpha, "bottom should fade out (top=$topAlpha bottom=$bottomAlpha)")
    }

    @Test
    fun shaderHasHorizontalHueVariation() {
        val bmp = render(time = 0f)
        val w = bmp.width
        val y = 6
        // Left vs right near the top should differ in color (hue spreads across width).
        val left = bmp.getColor(w / 8, y)
        val right = bmp.getColor(w * 7 / 8, y)
        fun rgb(c: Int) = Triple((c ushr 16) and 0xFF, (c ushr 8) and 0xFF, c and 0xFF)
        val (lr, lg, lb) = rgb(left)
        val (rr, rg, rb) = rgb(right)
        val diff = kotlin.math.abs(lr - rr) + kotlin.math.abs(lg - rg) + kotlin.math.abs(lb - rb)
        assertTrue(diff > 8, "left/right should differ in hue (left=$left right=$right diff=$diff)")
    }
}
