package ai.deneb.sensing

internal const val ACTION_BOOT_COMPLETED = "android.intent.action.BOOT_COMPLETED"
internal const val ACTION_MY_PACKAGE_REPLACED = "android.intent.action.MY_PACKAGE_REPLACED"
internal const val ACTION_USER_UNLOCKED = "android.intent.action.USER_UNLOCKED"

/**
 * Android clears app geofences across reboot. Restoring on boot fixes the "works until the
 * phone restarts" failure mode; USER_UNLOCKED gives a second chance when secure settings are
 * unavailable before first unlock, and MY_PACKAGE_REPLACED repairs after app updates.
 */
fun shouldRestoreGeofencesForAction(action: String?): Boolean = when (action) {
    ACTION_BOOT_COMPLETED,
    ACTION_MY_PACKAGE_REPLACED,
    ACTION_USER_UNLOCKED,
    null -> true,
    else -> false
}
