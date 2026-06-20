package ai.deneb.sensing

import kotlinx.serialization.Serializable
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.double
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/** JSON codec for the geofence list (stored as a string in AppSettings). */
fun encodeGeofences(list: List<DenebGeofence>): String = Json.encodeToString(list)

fun decodeGeofences(json: String): List<DenebGeofence> = runCatching { Json.decodeFromString<List<DenebGeofence>>(json) }.getOrDefault(emptyList())

/**
 * A user-pinned place geofence (집/직장). The user sets these by standing at the place
 * and tapping "현재 위치를 집/직장으로 설정" — id is stable ("home"/"work"), the lat/lng
 * come from [readCurrentLocation], radius defaults to 150m (a balance: tight enough to
 * mean "arrived", loose enough that GPS jitter doesn't miss it).
 */
@Serializable
data class DenebGeofence(
    val id: String,
    val label: String,
    val latitude: Double,
    val longitude: Double,
    val radiusM: Float = 150f,
)

/**
 * Register [geofences] with the OS, replacing any previously registered set. Android uses
 * GeofencingClient + a PendingIntent to DenebGeofenceReceiver, so ENTER/EXIT transitions
 * fire even when the app is backgrounded — which needs ACCESS_BACKGROUND_LOCATION ("항상
 * 허용"); without it the OS only delivers while the app is in the foreground. Returns false
 * when geofencing is unavailable or the permission is missing. Other targets are no-ops.
 */
expect suspend fun applyGeofences(geofences: List<DenebGeofence>): Boolean

/**
 * Build a geofence from a [readCurrentLocation] JSON fix (`{"latitude":..,"longitude":..}`).
 * Used by the "현재 위치를 집/직장으로 설정" buttons. Returns null if the JSON lacks coordinates.
 */
fun parseLocationToGeofence(id: String, label: String, locationJson: String, radiusM: Float = 150f): DenebGeofence? = runCatching {
    val obj = Json.parseToJsonElement(locationJson).jsonObject
    val lat = obj["latitude"]!!.jsonPrimitive.double
    val lng = obj["longitude"]!!.jsonPrimitive.double
    DenebGeofence(id, label, lat, lng, radiusM)
}.getOrNull()
