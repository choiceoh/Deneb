package ai.deneb.deneb

import ai.deneb.deneb.generated.DashboardOut
import kotlinx.serialization.json.buildJsonObject

/**
 * Dashboard surface of [DenebGatewayClient] (`miniapp.dashboard.*`). An extension
 * so the gateway client stays one facade while each RPC domain lives in its own
 * file (same split as [DenebClientCalendar] et al.).
 *
 * The dashboard groups every live work item (calendar events + work-feed cards)
 * into 파트(part) lanes — 기획조정실 1·2·3팀, 남도에코, 개인/기타 — so the operator
 * sees who is doing what at a glance. The gateway always returns the five fixed
 * lanes (empty ones included, in order) and appends '미분류' only when it has
 * items, so the screen renders the lanes in the order received without reshaping.
 */

/**
 * Fetch the part-grouped work dashboard (`miniapp.dashboard.lanes`, no params).
 * Returns null on a fetch failure so the screen can tell a real "no work" from a
 * network error instead of spinning forever (mirrors [refreshCalendar]).
 */
suspend fun DenebGatewayClient.fetchDashboardLanes(): DashboardOut? = callRpc<DashboardOut>("miniapp.dashboard.lanes", buildJsonObject {})
