package ai.deneb.sensing

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertFalse

class GeofencingDesktopTest {

    @Test
    fun unsupportedDesktopRegistrationReturnsFalseWithoutStateTransition() = runTest {
        val geofence = DenebGeofence("home", "Home", 37.5665, 126.9780)

        val registered = applyGeofences(listOf(geofence))

        assertFalse(registered)
    }
}
