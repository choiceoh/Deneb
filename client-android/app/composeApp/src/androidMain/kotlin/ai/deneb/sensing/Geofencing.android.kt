package ai.deneb.sensing

import ai.deneb.DenebGeofenceReceiver
import android.Manifest
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationServices
import kotlinx.coroutines.suspendCancellableCoroutine
import org.koin.java.KoinJavaComponent
import kotlin.coroutines.resume

/**
 * Register [geofences] via GeofencingClient, replacing the prior set. Requires
 * ACCESS_FINE_LOCATION (checked); background delivery additionally needs
 * ACCESS_BACKGROUND_LOCATION — without it the OS only fires while the app is foreground.
 * The PendingIntent targets [DenebGeofenceReceiver].
 */
actual suspend fun applyGeofences(geofences: List<DenebGeofence>): Boolean {
    val context = runCatching { KoinJavaComponent.get<Context>(Context::class.java) }.getOrNull() ?: return false
    if (ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
        return false
    }
    val client = LocationServices.getGeofencingClient(context)
    val pi = geofencePendingIntent(context)

    // Clear the prior set first so re-pinning a place never stacks duplicates.
    runCatching {
        suspendCancellableCoroutine { cont ->
            client.removeGeofences(pi).addOnCompleteListener { if (cont.isActive) cont.resume(Unit) }
        }
    }
    if (geofences.isEmpty()) return true

    val fences = geofences.map { g ->
        Geofence.Builder()
            .setRequestId(g.id)
            .setCircularRegion(g.latitude, g.longitude, g.radiusM)
            .setExpirationDuration(Geofence.NEVER_EXPIRE)
            .setTransitionTypes(Geofence.GEOFENCE_TRANSITION_ENTER or Geofence.GEOFENCE_TRANSITION_EXIT)
            .build()
    }
    val request = GeofencingRequest.Builder()
        .setInitialTrigger(GeofencingRequest.INITIAL_TRIGGER_ENTER)
        .addGeofences(fences)
        .build()

    return suspendCancellableCoroutine { cont ->
        @Suppress("MissingPermission") // FINE checked above; BACKGROUND only gates delivery, not registration
        client.addGeofences(request, pi)
            .addOnSuccessListener { if (cont.isActive) cont.resume(true) }
            .addOnFailureListener { if (cont.isActive) cont.resume(false) }
    }
}

private fun geofencePendingIntent(context: Context): PendingIntent {
    val intent = Intent(context, DenebGeofenceReceiver::class.java)
    // Geofencing requires a MUTABLE PendingIntent — the OS fills in the transition extras.
    return PendingIntent.getBroadcast(
        context,
        0,
        intent,
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
    )
}
