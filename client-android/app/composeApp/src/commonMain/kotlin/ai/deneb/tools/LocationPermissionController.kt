package ai.deneb.tools

import androidx.compose.runtime.Composable
import kotlinx.coroutines.flow.StateFlow

/**
 * Runtime location-permission gate (FINE/COARSE), mirroring [ContactsPermissionController].
 * Only the Android actual requests a real permission; location sensing ships on Android
 * (foss flavor), so other platforms are no-ops.
 */
expect class LocationPermissionController() {
    val permissionRequested: StateFlow<Boolean>
    fun hasPermission(): Boolean
    suspend fun requestPermission(): Boolean
    fun onPermissionResult(granted: Boolean)
}

@Composable
expect fun SetupLocationPermissionHandler(controller: LocationPermissionController)
