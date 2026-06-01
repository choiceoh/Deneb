package com.inspiredandroid.kai.tools

import androidx.compose.runtime.Composable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

actual class ContactsPermissionController actual constructor() {
    private val _permissionRequested = MutableStateFlow(false)
    actual val permissionRequested: StateFlow<Boolean> = _permissionRequested

    actual fun hasPermission(): Boolean = true
    actual suspend fun requestPermission(): Boolean = true
    actual fun onPermissionResult(granted: Boolean) {}
}

@Composable
actual fun SetupContactsPermissionHandler(controller: ContactsPermissionController) {}
