package ai.deneb.deneb

import ai.deneb.deneb.generated.RSILoopStatusResponse
import kotlinx.serialization.json.buildJsonObject

/**
 * Recursive self-improvement surface of [DenebGatewayClient]
 * (`miniapp.rsi.status`). An extension so the gateway client stays one facade
 * while each RPC domain lives in its own file (same split as
 * [DenebClientDashboard] et al.).
 *
 * Returns the four self-improvement loop layers (L1 skill evolution, L2 meta-
 * evolution, L3 verifier co-evolution, L4 source self-edit) with each one's
 * honest LIVE/DATA-GATED/STARVED/FROZEN/IDLE state — read-only.
 */

/**
 * Fetch the RSI loop status (`miniapp.rsi.status`, no params). Returns null on a
 * fetch failure so the screen can tell a real "no data" from a network error
 * instead of spinning forever (mirrors [fetchDashboardLanes]).
 */
suspend fun DenebGatewayClient.fetchRsiStatus(): RSILoopStatusResponse? = callRpc<RSILoopStatusResponse>("miniapp.rsi.status", buildJsonObject {})
