package ai.deneb

import ai.deneb.data.AppSettings
import ai.deneb.sensing.applyGeofences
import ai.deneb.sensing.decodeGeofences
import ai.deneb.sensing.shouldRestoreGeofencesForAction
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.java.KoinJavaComponent
import kotlin.time.Duration.Companion.seconds

/**
 * Restores persisted geofences after reboot/app replacement, before the user manually reopens
 * the app. Without this, Android drops the registrations on reboot and 집/직장 arrival notices
 * silently stop until the next launch.
 */
class DenebBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (!shouldRestoreGeofencesForAction(intent.action)) return

        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.Default).launch {
            try {
                withTimeoutOrNull(15.seconds) {
                    val appSettings = runCatching { KoinJavaComponent.get<AppSettings>(AppSettings::class.java) }.getOrNull()
                        ?: return@withTimeoutOrNull
                    val saved = decodeGeofences(appSettings.getGeofencesJson())
                    if (saved.isNotEmpty()) {
                        applyGeofences(saved)
                    }
                }
            } finally {
                pending.finish()
            }
        }
    }
}
