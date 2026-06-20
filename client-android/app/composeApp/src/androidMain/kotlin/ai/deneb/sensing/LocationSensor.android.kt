package ai.deneb.sensing

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import androidx.core.content.ContextCompat
import com.google.android.gms.location.CurrentLocationRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import kotlinx.coroutines.suspendCancellableCoroutine
import org.koin.java.KoinJavaComponent
import kotlin.coroutines.resume

/**
 * FusedLocationProvider one-shot read. Context comes from Koin (registered via
 * androidContext() in DenebApplication), mirroring UsageSensor. Returns null unless a
 * location permission (FINE or COARSE) is granted, or when no fix is available.
 *
 * Balanced-power priority with a 60s max age: a coarse/recent fix is plenty for
 * "where is he roughly" and cheap on battery. The result is compact JSON in the same
 * shape termux-location returned, so phone_read's downstream formatting is unchanged.
 */
actual suspend fun readCurrentLocation(): String? {
    val context = runCatching { KoinJavaComponent.get<Context>(Context::class.java) }.getOrNull() ?: return null
    val fine = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
    val coarse = ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED
    if (!fine && !coarse) return null

    val client = LocationServices.getFusedLocationProviderClient(context)
    val request = CurrentLocationRequest.Builder()
        .setPriority(Priority.PRIORITY_BALANCED_POWER_ACCURACY)
        .setMaxUpdateAgeMillis(60_000L)
        .build()

    val location: Location = suspendCancellableCoroutine { cont ->
        val task = client.getCurrentLocation(request, null)
        task.addOnSuccessListener { loc -> if (cont.isActive) cont.resume(loc) } // loc may be null
        task.addOnFailureListener { if (cont.isActive) cont.resume(null) }
    } ?: return null

    return buildString {
        append("{")
        append("\"latitude\":").append(location.latitude).append(",")
        append("\"longitude\":").append(location.longitude).append(",")
        append("\"accuracy\":").append(location.accuracy).append(",")
        append("\"provider\":\"").append(location.provider ?: "fused").append("\"")
        append("}")
    }
}
