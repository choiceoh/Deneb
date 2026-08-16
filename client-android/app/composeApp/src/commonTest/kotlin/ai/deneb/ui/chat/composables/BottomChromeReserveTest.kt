package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * The app shell reserves max(tabBar, IME) so the composer rides the keyboard
 * without a boolean tab-bar hide. These pin the endpoints that used to jump.
 */
class BottomChromeReserveTest {

    @Test
    fun keyboardDownReservesTheTabBar() {
        assertEquals(100, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 0))
    }

    @Test
    fun imeShorterThanTabBarKeepsTabBarReserve() {
        assertEquals(100, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 40))
    }

    @Test
    fun imeTallerThanTabBarReservesIme() {
        assertEquals(240, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 240))
    }

    @Test
    fun crossingTheTabBarHeightIsContinuous() {
        assertEquals(100, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 99))
        assertEquals(100, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 100))
        assertEquals(101, denebBottomChromeReservePx(tabBarFullPx = 100, imePx = 101))
    }
}
