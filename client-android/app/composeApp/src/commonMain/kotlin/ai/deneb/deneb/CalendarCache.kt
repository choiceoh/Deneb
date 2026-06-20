package ai.deneb.deneb

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

// --- Upcoming-calendar cache (cache-then-network) -------------------------------
// Mirrors the mail/work-feed caches: the now-anchored look-ahead list is persisted,
// owner-fingerprinted, so the calendar renders instantly on cold start and shows the
// last-known schedule when the gateway is unreachable — the offline-first launcher
// shell. The network refresh overwrites with the authoritative list. Reuses
// [mailCacheOwner] (the url#token account fingerprint).

private val calendarCacheJson = Json { ignoreUnknownKeys = true }

private const val CALENDAR_CACHE_MAX = 100

@Serializable
private data class CalendarCacheEnvelope(
    val owner: String = "",
    val events: List<CalendarEvent> = emptyList(),
)

internal fun encodeCalendarCache(events: List<CalendarEvent>, owner: String): String = calendarCacheJson.encodeToString(
    CalendarCacheEnvelope(owner = owner, events = events.take(CALENDAR_CACHE_MAX)),
)

internal fun decodeCalendarCache(json: String, expectedOwner: String): List<CalendarEvent>? = runCatching { calendarCacheJson.decodeFromString<CalendarCacheEnvelope>(json) }
    .getOrNull()
    ?.takeIf { it.owner == expectedOwner }
    ?.events
    ?.takeIf { it.isNotEmpty() }

internal fun DenebGatewayClient.loadCachedCalendar(): List<CalendarEvent>? {
    val json = appSettings.getCachedCalendar() ?: return null
    return decodeCalendarCache(json, mailCacheOwner(gatewayUrl, clientToken))
}

internal fun DenebGatewayClient.storeCachedCalendar(events: List<CalendarEvent>) {
    appSettings.putCachedCalendar(encodeCalendarCache(events, mailCacheOwner(gatewayUrl, clientToken)))
}
