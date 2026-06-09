package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

private const val URL = "deneb.gatewayUrl"
private const val TOKEN = "deneb.clientToken"
private val KEYS = setOf(URL, TOKEN)

class DurableMirrorSettingsTest {

    @Test
    fun writesMirroredKeysToBothStoresButNotOthers() {
        val delegate = MapSettings()
        val mirror = MapSettings()
        val s = DurableMirrorSettings(delegate, mirror, KEYS)

        s.putString(TOKEN, "secret")
        s.putString("apiKey", "sk-123")

        assertEquals("secret", delegate.getStringOrNull(TOKEN))
        assertEquals("secret", mirror.getStringOrNull(TOKEN)) // mirrored
        assertEquals("sk-123", delegate.getStringOrNull("apiKey"))
        assertNull(mirror.getStringOrNull("apiKey")) // not mirrored → stays encrypted-only
    }

    @Test
    fun healsDelegateFromMirrorAfterWipe() {
        val mirror = MapSettings()
        // first session: token written, plain mirror populated as a side effect
        DurableMirrorSettings(MapSettings(), mirror, KEYS).putString(TOKEN, "secret")
        assertEquals("secret", mirror.getStringOrNull(TOKEN))

        // app update wipes the encrypted store: fresh empty delegate, same plain mirror
        val freshDelegate = MapSettings()
        val s = DurableMirrorSettings(freshDelegate, mirror, KEYS)
        assertFalse(freshDelegate.hasKey(TOKEN))

        assertEquals("secret", s.getString(TOKEN, "")) // recovered from mirror
        assertEquals("secret", freshDelegate.getStringOrNull(TOKEN)) // and healed back into delegate
    }

    @Test
    fun backfillsMirrorFromDelegateOnRead() {
        // existing user upgrading to this build: token only in the encrypted store,
        // the new plain mirror is still empty
        val delegate = MapSettings().apply { putString(TOKEN, "secret") }
        val mirror = MapSettings()
        val s = DurableMirrorSettings(delegate, mirror, KEYS)
        assertFalse(mirror.hasKey(TOKEN))

        assertEquals("secret", s.getString(TOKEN, ""))
        assertEquals("secret", mirror.getStringOrNull(TOKEN)) // backfilled → next wipe is covered
    }

    @Test
    fun getStringOrNullSyncsMirroredKeys() {
        val delegate = MapSettings().apply { putString(URL, "http://gw:18789") }
        val mirror = MapSettings()
        val s = DurableMirrorSettings(delegate, mirror, KEYS)

        assertEquals("http://gw:18789", s.getStringOrNull(URL))
        assertEquals("http://gw:18789", mirror.getStringOrNull(URL)) // backfilled
    }

    @Test
    fun returnsDefaultWhenNeitherStoreHasKey() {
        val s = DurableMirrorSettings(MapSettings(), MapSettings(), KEYS)
        assertEquals("fallback", s.getString(URL, "fallback"))
        assertNull(s.getStringOrNull(URL))
    }

    @Test
    fun removeAndClearDropMirroredKeysFromBoth() {
        val delegate = MapSettings()
        val mirror = MapSettings()
        val s = DurableMirrorSettings(delegate, mirror, KEYS)
        s.putString(URL, "u")
        s.putString(TOKEN, "t")

        s.remove(URL)
        assertFalse(delegate.hasKey(URL))
        assertFalse(mirror.hasKey(URL))
        assertTrue(mirror.hasKey(TOKEN)) // other mirrored key unaffected

        s.clear()
        assertFalse(mirror.hasKey(TOKEN)) // clear drops mirrored keys from the mirror too
    }

    @Test
    fun nonMirroredKeysPassStraightThrough() {
        val delegate = MapSettings()
        val mirror = MapSettings()
        val s = DurableMirrorSettings(delegate, mirror, KEYS)

        s.putString("apiKey", "sk-123")
        s.putInt("count", 7)

        assertEquals("sk-123", s.getString("apiKey", ""))
        assertEquals(7, s.getInt("count", 0))
        assertNull(mirror.getStringOrNull("apiKey")) // never mirrored
        assertFalse(mirror.hasKey("count"))
    }
}
