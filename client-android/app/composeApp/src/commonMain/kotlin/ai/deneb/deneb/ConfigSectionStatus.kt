package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeStatusOut

/**
 * What a settings row reports about itself when it is NOT normal.
 *
 * The whole point is the null: a healthy section returns nothing, so the list stays
 * quiet and any mark on it means something. See ADR 0008 — colour is spent on
 * failure, and [failure] is what picks `colorScheme.error` over the warning ink.
 *
 * Unknown is also null. A probe that could not run (offline, timeout, an endpoint
 * that answered garbage) must not render as healthy OR as broken — the gateway row
 * already reports a lost connection, and inventing a fault from a failed probe would
 * teach the operator to distrust the marks.
 */
internal data class SectionStatus(val text: String, val failure: Boolean)

/**
 * Fleet: any unreachable node is a fault the operator has to act on — a down node
 * silently removes serving capacity (srv2's WiFi drops are the recurring case).
 * Reports the ratio rather than a word so the list says how bad it is.
 */
internal fun fleetSectionStatus(state: FleetState?): SectionStatus? {
    val nodes = state?.nodes ?: return null
    if (nodes.isEmpty()) return null
    val up = nodes.count { it.reachable }
    if (up == nodes.size) return null
    return SectionStatus("노드 $up/${nodes.size}", failure = true)
}

/**
 * Wormhole, worst-first: an unreachable router is a fault; active failover and
 * unhealthy keys are degradations that take the warning ink because the router is
 * still answering.
 *
 * Failover outranks key health. An open breaker means wormhole is serving that lane
 * from a DIFFERENT model than the one configured — answers keep arriving, so nothing
 * else in the app looks wrong, and the swap ran for 12 hours unnoticed once. A dead
 * key, by contrast, announces itself the moment something calls it.
 *
 * `circuitState` is empty when the live view is unavailable (the gateway's
 * config-file fallback has no breaker). Empty is unknown, so only an explicit "open"
 * counts — "degraded"/"half_open" are wormhole still preferring the primary.
 */
internal fun wormholeSectionStatus(status: WormholeStatusOut?): SectionStatus? {
    if (status == null) return null
    if (!status.reachable) return SectionStatus("라우터 응답 없음", failure = true)
    val failingOver = status.models.count { it.circuitState == "open" }
    if (failingOver > 0) return SectionStatus("페일오버 ${failingOver}건", failure = false)
    val problems = status.models.count { keyHealthIsProblem(it.keyHealth) }
    if (problems == 0) return null
    return SectionStatus("키 문제 ${problems}건", failure = false)
}
