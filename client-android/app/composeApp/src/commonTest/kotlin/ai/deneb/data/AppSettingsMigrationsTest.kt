package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Guards the one live edge kept after the `Service` provider model was removed:
 * the legacy app-open counter migration wired in `AppModule` via
 * `runMigrations(createLegacySettings())`.
 */
class AppSettingsMigrationsTest {

    @Test
    fun `legacy app_opens counter is ported on first run`() {
        val legacy = MapSettings()
        legacy.putInt(AppSettings.KEY_APP_OPENS, 7)

        val settings = MapSettings()
        AppSettings(settings).runMigrations(legacy)

        assertEquals(7, settings.getInt(AppSettings.KEY_APP_OPENS, 0))
        assertTrue(settings.getBoolean(AppSettings.KEY_MIGRATION_COMPLETE, false))
    }

    @Test
    fun `migration does not overwrite an existing local app_opens value`() {
        val legacy = MapSettings()
        legacy.putInt(AppSettings.KEY_APP_OPENS, 7)

        val settings = MapSettings()
        settings.putInt(AppSettings.KEY_APP_OPENS, 99)
        AppSettings(settings).runMigrations(legacy)

        assertEquals(99, settings.getInt(AppSettings.KEY_APP_OPENS, 0))
    }

    @Test
    fun `migration runs only once`() {
        val legacy = MapSettings()
        legacy.putInt(AppSettings.KEY_APP_OPENS, 7)

        val settings = MapSettings()
        val appSettings = AppSettings(settings)
        appSettings.runMigrations(legacy)

        // Second run with a fresh legacy value must be ignored (flag already set).
        legacy.putInt(AppSettings.KEY_APP_OPENS, 42)
        appSettings.runMigrations(legacy)

        assertEquals(7, settings.getInt(AppSettings.KEY_APP_OPENS, 0))
    }

    @Test
    fun `null legacy store is a no-op (desktop and wasm)`() {
        val settings = MapSettings()
        AppSettings(settings).runMigrations(null)

        assertFalse(settings.getBoolean(AppSettings.KEY_MIGRATION_COMPLETE, false))
        assertFalse(settings.hasKey(AppSettings.KEY_APP_OPENS))
    }
}
