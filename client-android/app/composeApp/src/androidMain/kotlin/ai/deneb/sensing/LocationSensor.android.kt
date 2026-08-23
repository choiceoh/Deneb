package ai.deneb.sensing

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.location.Geocoder
import android.location.Location
import androidx.core.content.ContextCompat
import com.google.android.gms.location.CurrentLocationRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.koin.java.KoinJavaComponent
import java.util.Locale
import kotlin.coroutines.resume

// A one-shot fix and a best-effort address are both "nice to have, now": bound
// them so a caller never waits on a fix that will not arrive.
private const val FIX_TIMEOUT_MS = 15_000L
private const val GEOCODE_TIMEOUT_MS = 5_000L

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
        // Without this the request's duration defaults to Long.MAX_VALUE: indoors,
        // with no fix to be had, the settings screen would sit on "요청 중…"
        // forever instead of reporting that it could not get one.
        .setDurationMillis(FIX_TIMEOUT_MS)
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
        // On-device reverse geocoding: the gateway matches this Korean admin
        // string ("전라북도 군산시 옥구읍 수산리") against project 현장 to log a
        // site visit. Best-effort — omitted on failure (no network, no result).
        reverseGeocodeOffMain(context, location.latitude, location.longitude)?.let {
            append(",\"place\":\"").append(jsonEscape(it)).append("\"")
        }
        readBatteryJson(context)?.let { append(",\"battery\":").append(it) }
        append("}")
    }
}

/**
 * [reverseGeocode] off the caller's thread and under a deadline.
 *
 * The blocking `getFromLocation` overload does network I/O when the area is not
 * cached, and one caller (the gateway settings card's 위치 핀) invokes this from a
 * `rememberCoroutineScope()` — i.e. the main thread — so without the hop it is a
 * StrictMode violation and a visible stall. Best-effort field: a timeout just
 * omits `place`.
 */
private suspend fun reverseGeocodeOffMain(context: Context, lat: Double, lng: Double): String? = withTimeoutOrNull(GEOCODE_TIMEOUT_MS) {
    withContext(Dispatchers.IO) { reverseGeocode(context, lat, lng) }
}

/**
 * Reverse-geocode a fix to a compact Korean administrative string using the
 * on-device [Geocoder] (no API key, works offline for cached areas). Joins the
 * admin components most likely to match a stored 현장 — adminArea (도), locality
 * (시/군), subLocality (읍/면/동), thoroughfare/feature (리) — deduped. Returns
 * null when geocoding is unavailable or yields nothing.
 */
@Suppress("DEPRECATION") // getFromLocation(lat,lng,n) is deprecated on API 33+ but works on all; the async variant adds callback complexity for a best-effort field
private fun reverseGeocode(context: Context, lat: Double, lng: Double): String? = runCatching {
    if (!Geocoder.isPresent()) return null
    val addr = Geocoder(context, Locale.KOREA).getFromLocation(lat, lng, 1)?.firstOrNull() ?: return null
    listOfNotNull(addr.adminArea, addr.subAdminArea, addr.locality, addr.subLocality, addr.thoroughfare, addr.featureName)
        .map { it.trim() }
        .filter { it.isNotEmpty() }
        .distinct()
        .joinToString(" ")
        .ifBlank { null }
}.getOrNull()

/** Escapes a string for embedding as a JSON string value (quotes/backslashes/newlines). */
private fun jsonEscape(s: String): String = s.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", " ").replace("\r", " ")

/**
 * Battery status as a compact JSON object, embedded in the location fix so the
 * gateway's single phone-state cache serves phone_read("battery") without any
 * extra permission or round-trip (the Termux/SSH read path is retired). Reads
 * the sticky ACTION_BATTERY_CHANGED broadcast — no receiver registration kept,
 * no permission needed. Null on any failure (field simply omitted).
 */
private fun readBatteryJson(context: Context): String? = runCatching {
    val intent = context.registerReceiver(
        null,
        android.content.IntentFilter(Intent.ACTION_BATTERY_CHANGED),
    ) ?: return null
    val level = intent.getIntExtra(android.os.BatteryManager.EXTRA_LEVEL, -1)
    val scale = intent.getIntExtra(android.os.BatteryManager.EXTRA_SCALE, -1)
    if (level < 0 || scale <= 0) return null
    val status = intent.getIntExtra(android.os.BatteryManager.EXTRA_STATUS, -1)
    val charging = status == android.os.BatteryManager.BATTERY_STATUS_CHARGING ||
        status == android.os.BatteryManager.BATTERY_STATUS_FULL
    "{\"percent\":${(level * 100) / scale},\"charging\":$charging}"
}.getOrNull()
