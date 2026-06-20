package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * The launcher pinned-apps store ([AppSettings.togglePinnedApp] / [pinnedAppsFlow]):
 * the 자체앱 favorites home and the app drawer both observe it, so order, idempotent
 * toggling, and persistence across instances must hold.
 */
class AppSettingsPinnedAppsTest {

    @Test
    fun `toggle pins then unpins, preserving pin order`() {
        val app = AppSettings(MapSettings())
        app.togglePinnedApp("com.kakao.talk")
        app.togglePinnedApp("com.android.dialer")
        assertEquals(listOf("com.kakao.talk", "com.android.dialer"), app.getPinnedApps())

        // Toggling an existing pin removes it; the rest keep their order.
        app.togglePinnedApp("com.kakao.talk")
        assertEquals(listOf("com.android.dialer"), app.getPinnedApps())
    }

    @Test
    fun `flow publishes the latest pin set`() {
        val app = AppSettings(MapSettings())
        app.togglePinnedApp("a")
        app.togglePinnedApp("b")
        assertEquals(listOf("a", "b"), app.pinnedAppsFlow.value)
    }

    @Test
    fun `pins persist across instances on the same store`() {
        val store = MapSettings()
        AppSettings(store).togglePinnedApp("com.example.app")
        // A fresh AppSettings over the same store reloads the pins (init reads the key).
        assertEquals(listOf("com.example.app"), AppSettings(store).getPinnedApps())
    }

    @Test
    fun `blank package is ignored`() {
        val app = AppSettings(MapSettings())
        app.togglePinnedApp("")
        assertTrue(app.getPinnedApps().isEmpty())
    }
}
