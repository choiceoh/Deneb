package ai.deneb.deneb

// --- Upcoming-calendar cache (cache-then-network) -------------------------------
// Mirrors the mail/work-feed caches: the now-anchored look-ahead list is persisted,
// owner-fingerprinted, so the calendar renders instantly on cold start and shows the
// last-known schedule when the gateway is unreachable — the offline-first launcher
// shell. The network refresh overwrites with the authoritative list. Reuses
// [mailCacheOwner] (the url#token account fingerprint).

private const val CALENDAR_CACHE_MAX = 100

private val calendarCacheCodec = OwnedListCacheCodec("events", CalendarEvent.serializer(), CALENDAR_CACHE_MAX)

internal fun encodeCalendarCache(events: List<CalendarEvent>, owner: String): String = calendarCacheCodec.encode(events, owner)

internal fun decodeCalendarCache(json: String, expectedOwner: String): List<CalendarEvent>? = calendarCacheCodec.decode(json, expectedOwner)

internal fun DenebGatewayClient.loadCachedCalendar(): List<CalendarEvent>? {
    val json = appSettings.getCachedCalendar() ?: return null
    return decodeCalendarCache(json, mailCacheOwner(gatewayUrl, clientToken))
}

internal fun DenebGatewayClient.storeCachedCalendar(events: List<CalendarEvent>) {
    appSettings.putCachedCalendar(encodeCalendarCache(events, mailCacheOwner(gatewayUrl, clientToken)))
}
