package ai.deneb.sensing

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class GeofenceRestoreTest {
    @Test
    fun restoresOnBootLikeActions() {
        assertTrue(shouldRestoreGeofencesForAction(ACTION_BOOT_COMPLETED))
        assertTrue(shouldRestoreGeofencesForAction(ACTION_USER_UNLOCKED))
        assertTrue(shouldRestoreGeofencesForAction(ACTION_MY_PACKAGE_REPLACED))
    }

    @Test
    fun ignoresUnrelatedActions() {
        assertFalse(shouldRestoreGeofencesForAction("android.intent.action.TIMEZONE_CHANGED"))
    }

    @Test
    fun restoresOnColdStart() {
        assertTrue(shouldRestoreGeofencesForAction(null))
    }
}
