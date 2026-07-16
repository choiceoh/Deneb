package ai.deneb.deneb

import ai.deneb.deneb.generated.ProjectDigestsOut
import ai.deneb.deneb.generated.ProjectSiteSetStatusOut
import ai.deneb.deneb.generated.ProjectSitesOut
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

/**
 * Project surface of [DenebGatewayClient] (`miniapp.project.*`). An extension so
 * the gateway client stays one facade while each RPC domain lives in its own file
 * (same split as [DenebClientDashboard] et al.).
 *
 * The digests are produced offline by the wiki dream cycle (one LLM roll-up per
 * cycle, server-side), so this read is cheap and instant — no LLM on the path.
 */

/**
 * Fetch each active project's latest-progress digest (`miniapp.project.digests`,
 * no params), newest first. Returns null on a fetch failure so the screen can
 * tell a real "no digests yet" from a network error instead of spinning forever
 * (mirrors [fetchDashboardLanes]).
 */
suspend fun DenebGatewayClient.fetchProjectDigests(): ProjectDigestsOut? = callRpc<ProjectDigestsOut>("miniapp.project.digests", buildJsonObject {})

/**
 * Fetch every active project that carries a 현장 (`miniapp.project.sites`, no params),
 * for the 현장 지도. Unlike the digests, this includes projects with no 현재 상태 yet,
 * so the map shows all current sites. Returns null on a fetch failure (mirrors
 * [fetchProjectDigests]).
 */
suspend fun DenebGatewayClient.fetchProjectSites(): ProjectSitesOut? = callRpc<ProjectSitesOut>("miniapp.project.sites", buildJsonObject {})

/**
 * Set a 현장 page's lifecycle status (`miniapp.project.site.setStatus`).
 * [status] is 후보/계약/개설/준공, or "" to clear to 미분류. Only real 현장 pages
 * are writable — 대표페이지 fallback pins reject. Returns null on failure.
 */
suspend fun DenebGatewayClient.setProjectSiteStatus(path: String, status: String): ProjectSiteSetStatusOut? = callRpc<ProjectSiteSetStatusOut>(
    "miniapp.project.site.setStatus",
    buildJsonObject {
        put("path", path)
        put("status", status)
    },
)
