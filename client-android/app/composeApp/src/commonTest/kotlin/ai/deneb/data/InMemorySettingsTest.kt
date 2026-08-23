package ai.deneb.data

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The degraded-mode settings store. It only runs when the encrypted store cannot
 * be opened at all, so what matters is that the app boots and reads sane defaults
 * — a wrong default here would look like a corrupted setting, not a missing store.
 */
class InMemorySettingsTest {

    @Test
    fun roundTripsEveryValueType() {
        val settings = InMemorySettings()

        settings.putInt("i", 7)
        settings.putLong("l", 8L)
        settings.putString("s", "값")
        settings.putFloat("f", 1.5f)
        settings.putDouble("d", 2.5)
        settings.putBoolean("b", true)

        assertEquals(7, settings.getInt("i", 0))
        assertEquals(8L, settings.getLong("l", 0L))
        assertEquals("값", settings.getString("s", ""))
        assertEquals(1.5f, settings.getFloat("f", 0f))
        assertEquals(2.5, settings.getDouble("d", 0.0))
        assertTrue(settings.getBoolean("b", false))
        assertEquals(6, settings.size)
    }

    @Test
    fun missingKeysFallBackToTheCallersDefault() {
        val settings = InMemorySettings()

        assertEquals(42, settings.getInt("absent", 42))
        assertEquals("기본", settings.getString("absent", "기본"))
        assertNull(settings.getStringOrNull("absent"))
        assertFalse(settings.hasKey("absent"))
    }

    @Test
    fun aKeyStoredAsAnotherTypeReadsAsAbsentRatherThanThrowing() {
        // A degraded store must never be the thing that crashes the app.
        val settings = InMemorySettings()
        settings.putString("k", "not a number")

        assertEquals(5, settings.getInt("k", 5))
        assertNull(settings.getIntOrNull("k"))
    }

    @Test
    fun removeAndClearDropKeys() {
        val settings = InMemorySettings()
        settings.putString("a", "1")
        settings.putString("b", "2")

        settings.remove("a")
        assertEquals(setOf("b"), settings.keys)

        settings.clear()
        assertEquals(0, settings.size)
        assertTrue(settings.keys.isEmpty())
    }
}
