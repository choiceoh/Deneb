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
 * Wormhole: an unreachable router is a fault; a reachable router with unhealthy keys
 * is degraded, not down, so it takes the warning ink.
 *
 * Deliberately NOT reported: "failover is active". Failover is the condition most
 * worth surfacing — it silently swaps the serving model — but `WormholeStatusOut`
 * carries no flag for it, and deriving one from `source` would be a guess dressed as
 * a fact. It lands when the wire says so.
 */
internal fun wormholeSectionStatus(status: WormholeStatusOut?): SectionStatus? {
    if (status == null) return null
    if (!status.reachable) return SectionStatus("라우터 응답 없음", failure = true)
    val problems = status.models.count { keyHealthIsProblem(it.keyHealth) }
    if (problems == 0) return null
    return SectionStatus("키 문제 ${problems}건", failure = false)
}
