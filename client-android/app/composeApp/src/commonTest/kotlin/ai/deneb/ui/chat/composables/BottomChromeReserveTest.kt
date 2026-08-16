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

    @Test
    fun chatSurfaceHidesTheTabBar() {
        assertEquals(false, denebShowsBottomBar(ROUTE_MAIN, ROUTE_HOME))
        assertEquals(false, denebShowsBottomBar(ROUTE_HOME, ROUTE_HOME))
    }

    @Test
    fun feedAndSectionsKeepTheTabBar() {
        assertEquals(true, denebShowsBottomBar(ROUTE_MAIN, ROUTE_FEED))
        assertEquals(true, denebShowsBottomBar(ROUTE_FEED, ROUTE_FEED))
        assertEquals(true, denebShowsBottomBar(ROUTE_MAIL, ROUTE_MAIL))
        assertEquals(false, denebShowsBottomBar("deneb_mail_detail", ROUTE_MAIL))
    }
}
