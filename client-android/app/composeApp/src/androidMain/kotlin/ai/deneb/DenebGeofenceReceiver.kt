package ai.deneb

import ai.deneb.data.DataRepository
import ai.deneb.deneb.DenebGatewayClient
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofencingEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.java.KoinJavaComponent
import kotlin.time.Duration.Companion.seconds

/**
 * Receives geofence ENTER/EXIT transitions (집/직장) and forwards them to the gateway as a
 * normal phone event (type "location", source = the place label) so the proactive 비서실장
 * judgment runs and can surface "직장 도착". Registered in the foss manifest; fires even when
 * the app is backgrounded if ACCESS_BACKGROUND_LOCATION is granted. goAsync() keeps the
 * process alive for the fire-and-forget POST.
 */
class DenebGeofenceReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val event = GeofencingEvent.fromIntent(intent) ?: return
        if (event.hasError()) return
        val verb = when (event.geofenceTransition) {
            Geofence.GEOFENCE_TRANSITION_ENTER -> "도착"
            Geofence.GEOFENCE_TRANSITION_EXIT -> "출발"
            else -> return
        }
        val ids = event.triggeringGeofences?.map { it.requestId }.orEmpty()
        if (ids.isEmpty()) return
        val client = runCatching { KoinJavaComponent.get<DataRepository>(DataRepository::class.java) }
            .getOrNull() as? DenebGatewayClient ?: return

        val pending = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.Default).launch {
            try {
                withTimeoutOrNull(10.seconds) {
                    for (id in ids) {
                        val label = geofenceLabel(id)
                        runCatching { client.ingestEvent("location", label, "$label $verb") }
                    }
                }
            } finally {
                pending.finish()
            }
        }
    }
}

// requestId is the stable geofence id ("home"/"work"); map to the Korean event source.
private fun geofenceLabel(id: String): String = when (id) {
    "home" -> "집"
    "work" -> "직장"
    else -> id
}
