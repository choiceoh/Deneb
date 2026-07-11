package ai.deneb.data

import com.russhwolf.settings.MapSettings
import com.russhwolf.settings.Settings
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class DurableMirrorSettingsFailureTest {

    private class FaultSettings(
        private val delegate: Settings = MapSettings(),
    ) : Settings by delegate {
        var failPutKey: String? = null
        var failRemoveKey: String? = null
        var failClear = false

        override fun putString(key: String, value: String) {
            if (key == failPutKey) error("put failed: $key")
            delegate.putString(key, value)
        }

        override fun remove(key: String) {
            if (key == failRemoveKey) error("remove failed: $key")
            delegate.remove(key)
        }

        override fun clear() {
            if (failClear) error("clear failed")
            delegate.clear()
        }
    }

    private val url = "deneb.gatewayUrl"
    private val token = "deneb.clientToken"
    private val keys = setOf(url, token)

    @Test
    fun gatewayWhitelistContainsOnlyPinnedIdentityKeys() {
        assertEquals(keys, DurableMirrorSettings.GATEWAY_KEYS)
    }

    @Test
    fun delegateWinsWhenBothStoresContainDifferentValues() {
        val delegate = MapSettings().apply { putString(token, "new") }
        val mirror = MapSettings().apply { putString(token, "old") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertEquals("new", settings.getString(token, ""))
        assertEquals("new", mirror.getString(token, ""))
    }

    @Test
    fun getStringOrNullAlsoRepairsAStaleMirror() {
        val delegate = MapSettings().apply { putString(url, "https://new") }
        val mirror = MapSettings().apply { putString(url, "https://old") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertEquals("https://new", settings.getStringOrNull(url))
        assertEquals("https://new", mirror.getString(url, ""))
    }

    @Test
    fun authoritativeEmptyStringReplacesStaleNonEmptyMirror() {
        val delegate = MapSettings().apply { putString(token, "") }
        val mirror = MapSettings().apply { putString(token, "old-secret") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertEquals("", settings.getString(token, "fallback"))
        assertEquals("", mirror.getString(token, "missing"))
    }

    @Test
    fun partialMirrorWriteIsReconciledOnNextRead() {
        val delegate = FaultSettings().apply { putString(token, "old") }
        val mirror = FaultSettings().apply {
            putString(token, "old")
            failPutKey = token
        }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.putString(token, "new") }
        assertEquals("new", delegate.getString(token, ""))
        assertEquals("old", mirror.getString(token, ""))

        mirror.failPutKey = null
        assertEquals("new", settings.getString(token, ""))
        assertEquals("new", mirror.getString(token, ""))
    }

    @Test
    fun delegateWriteFailureDoesNotAdvanceMirror() {
        val delegate = FaultSettings().apply {
            putString(token, "old")
            failPutKey = token
        }
        val mirror = MapSettings().apply { putString(token, "old") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.putString(token, "new") }

        assertEquals("old", delegate.getString(token, ""))
        assertEquals("old", mirror.getString(token, ""))
    }

    @Test
    fun nonMirroredWriteNeverTouchesFailingMirror() {
        val delegate = MapSettings()
        val mirror = FaultSettings().apply { failPutKey = "ordinary" }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        settings.putString("ordinary", "value")

        assertEquals("value", delegate.getString("ordinary", ""))
        assertFalse(mirror.hasKey("ordinary"))
    }

    @Test
    fun failedMirrorBackfillPropagatesAndLeavesDelegateAuthoritative() {
        val delegate = MapSettings().apply { putString(token, "secret") }
        val mirror = FaultSettings().apply { failPutKey = token }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.getString(token, "") }

        assertEquals("secret", delegate.getString(token, ""))
        assertFalse(mirror.hasKey(token))
    }

    @Test
    fun failedDelegateHealPropagatesWithoutRemovingRecoveryCopy() {
        val delegate = FaultSettings().apply { failPutKey = token }
        val mirror = MapSettings().apply { putString(token, "secret") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.getStringOrNull(token) }

        assertFalse(delegate.hasKey(token))
        assertEquals("secret", mirror.getString(token, ""))
    }

    @Test
    fun failedMirrorRemovalLeavesDelegateIntact() {
        val delegate = MapSettings().apply { putString(token, "secret") }
        val mirror = FaultSettings().apply {
            putString(token, "secret")
            failRemoveKey = token
        }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.remove(token) }

        assertEquals("secret", delegate.getString(token, ""))
        assertEquals("secret", mirror.getString(token, ""))
    }

    @Test
    fun removalSucceedsAcrossBothStoresWhenHealthy() {
        val delegate = MapSettings().apply { putString(token, "secret") }
        val mirror = MapSettings().apply { putString(token, "secret") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        settings.remove(token)

        assertFalse(delegate.hasKey(token))
        assertFalse(mirror.hasKey(token))
        assertNull(settings.getStringOrNull(token))
    }

    @Test
    fun nonMirroredRemoveBypassesFailingMirror() {
        val delegate = MapSettings().apply { putString("ordinary", "value") }
        val mirror = FaultSettings().apply { failRemoveKey = "ordinary" }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        settings.remove("ordinary")

        assertFalse(delegate.hasKey("ordinary"))
    }

    @Test
    fun failedFirstMirrorRemovalAbortsClearBeforeDelegateDataLoss() {
        val delegate = MapSettings().apply {
            putString(url, "u")
            putString(token, "t")
            putString("ordinary", "keep-on-failure")
        }
        val mirror = FaultSettings().apply {
            putString(url, "u")
            putString(token, "t")
            failRemoveKey = url
        }
        val settings = DurableMirrorSettings(delegate, mirror, linkedSetOf(url, token))

        assertFailsWith<IllegalStateException> { settings.clear() }

        assertEquals("u", delegate.getString(url, ""))
        assertEquals("t", delegate.getString(token, ""))
        assertEquals("keep-on-failure", delegate.getString("ordinary", ""))
    }

    @Test
    fun failedLaterMirrorRemovalStillKeepsDelegateUntouched() {
        val delegate = MapSettings().apply {
            putString(url, "u")
            putString(token, "t")
        }
        val mirror = FaultSettings().apply {
            putString(url, "u")
            putString(token, "t")
            failRemoveKey = token
        }
        val settings = DurableMirrorSettings(delegate, mirror, linkedSetOf(url, token))

        assertFailsWith<IllegalStateException> { settings.clear() }

        assertEquals("u", delegate.getString(url, ""))
        assertEquals("t", delegate.getString(token, ""))
        assertFalse(mirror.hasKey(url))
        assertTrue(mirror.hasKey(token))
    }

    @Test
    fun delegateClearFailureLeavesAlreadyRemovedMirrorRecoverableFromDelegate() {
        val delegate = FaultSettings().apply {
            putString(token, "secret")
            failClear = true
        }
        val mirror = MapSettings().apply { putString(token, "secret") }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertFailsWith<IllegalStateException> { settings.clear() }
        assertFalse(mirror.hasKey(token))
        assertEquals("secret", delegate.getString(token, ""))

        delegate.failClear = false
        assertEquals("secret", settings.getString(token, ""))
        assertEquals("secret", mirror.getString(token, ""))
    }

    @Test
    fun clearDoesNotEraseUnrelatedValuesFromSharedMirror() {
        val delegate = MapSettings().apply { putString(token, "secret") }
        val mirror = MapSettings().apply {
            putString(token, "secret")
            putString("shared-metadata", "keep")
        }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        settings.clear()

        assertEquals("keep", mirror.getString("shared-metadata", ""))
        assertFalse(mirror.hasKey(token))
    }

    @Test
    fun conflictingUrlAndTokenAreReconciledIndependently() {
        val delegate = MapSettings().apply {
            putString(url, "new-url")
            putString(token, "new-token")
        }
        val mirror = MapSettings().apply {
            putString(url, "old-url")
            putString(token, "old-token")
        }
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertEquals("new-url", settings.getString(url, ""))
        assertEquals("new-token", settings.getString(token, ""))
        assertEquals("new-url", mirror.getString(url, ""))
        assertEquals("new-token", mirror.getString(token, ""))
    }

    @Test
    fun defaultValueIsNotPersistedWhenBothStoresAreAbsent() {
        val delegate = MapSettings()
        val mirror = MapSettings()
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        assertEquals("fallback", settings.getString(token, "fallback"))

        assertFalse(delegate.hasKey(token))
        assertFalse(mirror.hasKey(token))
    }

    @Test
    fun nonStringSettingsOperationsRemainDelegateOnly() {
        val delegate = MapSettings()
        val mirror = MapSettings()
        val settings = DurableMirrorSettings(delegate, mirror, keys)

        settings.putInt(token, 7)
        settings.putBoolean(url, true)

        assertEquals(7, delegate.getInt(token, 0))
        assertTrue(delegate.getBoolean(url, false))
        assertFalse(mirror.hasKey(token))
        assertFalse(mirror.hasKey(url))
    }
}
