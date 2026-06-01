package com.inspiredandroid.kai.tools

import androidx.compose.runtime.Composable
import kotlinx.coroutines.flow.StateFlow

/**
 * Runtime READ_CONTACTS permission gate, mirroring [NotificationPermissionController].
 * Only the Android actual requests a real permission; the contacts-sync feature
 * ships on Android (foss flavor), so other platforms are no-ops.
 */
expect class ContactsPermissionController() {
    val permissionRequested: StateFlow<Boolean>
    fun hasPermission(): Boolean
    suspend fun requestPermission(): Boolean
    fun onPermissionResult(granted: Boolean)
}

@Composable
expect fun SetupContactsPermissionHandler(controller: ContactsPermissionController)
