package ai.deneb.deneb

import ai.deneb.deneb.generated.UsageStatsResult
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Token-usage surface of [DenebGatewayClient] (`miniapp.usage.stats`). An
 * extension so the gateway client stays one facade while each RPC domain lives
 * in its own file (same split as [DenebClientRsi] et al.).
 *
 * Returns token usage over the last [days] days broken down three ways — by the
 * model that answered, by the role the run requested, and by work-type. Returns
 * null on a fetch failure so the screen can tell a real "no data" from a network
 * error instead of spinning forever.
 */
suspend fun DenebGatewayClient.fetchUsageStats(days: Int): UsageStatsResult? = callRpc<UsageStatsResult>("miniapp.usage.stats", buildJsonObject { put("days", days) })
