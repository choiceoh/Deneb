package ai.deneb.deneb

import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Guards the model-role "?" tooltip positioning against the regression where a
 * wide tooltip anchored near the left edge of a narrow phone screen clipped off
 * the left (Material3's default provider returned a negative x there).
 */
class ClampedTooltipPositionProviderTest {
    private val provider = ClampedTooltipPositionProvider(verticalSpacing = 12)

    // 412dp-wide phone, the "?" anchor sits just right of the "역할별 모델" label
    // (near the left edge), and the role-description tooltip is wide. The clamped
    // provider must keep the tooltip fully on screen (x in [0, windowW - tipW]).
    @Test
    fun wideTooltipNearLeftEdgeStaysOnScreen() {
        val pos = provider.calculatePosition(
            anchorBounds = IntRect(left = 92, top = 200, right = 110, bottom = 218),
            windowSize = IntSize(width = 412, height = 915),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 320, height = 150),
        )
        assertTrue(pos.x >= 0, "tooltip x must not be negative, was ${pos.x}")
        assertTrue(
            pos.x + 320 <= 412,
            "tooltip right edge must stay on screen, was ${pos.x + 320}",
        )
    }

    // A tooltip wider than the whole window pins to the left edge (x == 0) instead
    // of going negative.
    @Test
    fun tooltipWiderThanScreenPinsToLeftEdge() {
        val pos = provider.calculatePosition(
            anchorBounds = IntRect(left = 92, top = 200, right = 110, bottom = 218),
            windowSize = IntSize(width = 412, height = 915),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 500, height = 150),
        )
        assertEquals(0, pos.x)
    }

    // Prefers sitting above the anchor when there is room: y = top - tipH - spacing.
    @Test
    fun prefersAboveAnchorWhenRoom() {
        val pos = provider.calculatePosition(
            anchorBounds = IntRect(left = 92, top = 400, right = 110, bottom = 418),
            windowSize = IntSize(width = 412, height = 915),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 320, height = 150),
        )
        assertEquals(400 - 150 - 12, pos.y)
    }

    // Falls back to below the anchor when sitting above would clip the top edge.
    @Test
    fun fallsBelowWhenNoRoomAbove() {
        val pos = provider.calculatePosition(
            anchorBounds = IntRect(left = 92, top = 40, right = 110, bottom = 58),
            windowSize = IntSize(width = 412, height = 915),
            layoutDirection = LayoutDirection.Ltr,
            popupContentSize = IntSize(width = 320, height = 150),
        )
        assertEquals(58 + 12, pos.y)
    }
}
