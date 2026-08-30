package ai.deneb.deneb

import ai.deneb.deneb.generated.WormholeModelOut
import ai.deneb.deneb.generated.WormholeStatusOut
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The settings list spends colour on failure only, so these functions are judged on
 * what they return NOTHING for as much as on what they report. Two silences matter
 * and are asserted separately: healthy (nothing is wrong) and unknown (the probe
 * could not run). Collapsing unknown into either direction is the bug this guards —
 * a fault invented from a failed probe teaches the operator to ignore the marks, and
 * a healthy mark for an unreachable service is a silent failure.
 */
class ConfigSectionStatusTest {
    private fun node(name: String, reachable: Boolean) = FleetNode(name = name, reachable = reachable)

    @Test
    fun unreachableNodesReportTheRatioAsAFailure() {
        val status = fleetSectionStatus(
            FleetState(listOf(node("srv1", true), node("srv2", false), node("srv4", true))),
        )
        assertEquals("노드 2/3", status?.text)
        assertTrue(status?.failure == true)
    }

    @Test
    fun aHealthyFleetStaysSilent() {
        assertNull(fleetSectionStatus(FleetState(listOf(node("srv1", true), node("srv4", true)))))
    }

    @Test
    fun anUnknownFleetIsNotReportedAsHealthyOrBroken() {
        assertNull(fleetSectionStatus(null))
        assertNull(fleetSectionStatus(FleetState(emptyList())))
    }

    @Test
    fun anUnreachableRouterIsAFailure() {
        val status = wormholeSectionStatus(WormholeStatusOut(reachable = false))
        assertEquals("라우터 응답 없음", status?.text)
        assertTrue(status?.failure == true)
    }

    @Test
    fun keyProblemsOnAReachableRouterAreDegradedNotDown() {
        val status = wormholeSectionStatus(
            WormholeStatusOut(
                reachable = true,
                models = listOf(
                    WormholeModelOut(name = "a", keyHealth = "auth_failed"),
                    WormholeModelOut(name = "b", keyHealth = "ok"),
                    WormholeModelOut(name = "c", keyHealth = "rate_limited"),
                ),
            ),
        )
        assertEquals("키 문제 2건", status?.text)
        assertTrue(status?.failure == false, "degraded takes the warning ink, not error")
    }

    @Test
    fun aHealthyRouterStaysSilent() {
        assertNull(
            wormholeSectionStatus(
                WormholeStatusOut(reachable = true, models = listOf(WormholeModelOut(name = "a", keyHealth = "ok"))),
            ),
        )
    }

    @Test
    fun anUncheckedKeyIsNotAProblem() {
        // "unchecked" means nobody looked yet — reporting it would fire on every
        // fresh boot and train the operator to ignore the mark.
        assertNull(
            wormholeSectionStatus(
                WormholeStatusOut(reachable = true, models = listOf(WormholeModelOut(name = "a", keyHealth = "unchecked"))),
            ),
        )
    }

    @Test
    fun anOpenBreakerReportsFailoverAndOutranksKeyHealth() {
        // The lane is being served by a different model than configured. Nothing
        // else in the app looks wrong when this happens, which is the whole reason
        // it belongs on the list.
        val status = wormholeSectionStatus(
            WormholeStatusOut(
                reachable = true,
                models = listOf(
                    WormholeModelOut(name = "a", keyHealth = "auth_failed"),
                    WormholeModelOut(name = "b", circuitState = "open"),
                ),
            ),
        )
        assertEquals("페일오버 1건", status?.text)
        assertTrue(status?.failure == false, "the router still answers — degraded, not down")
    }

    @Test
    fun onlyAnOpenBreakerCountsAsFailover() {
        // "degraded"/"half_open" still prefer the primary, and "" means the live
        // view was unavailable — neither is a swap.
        for (state in listOf("closed", "degraded", "half_open", "")) {
            assertNull(
                wormholeSectionStatus(
                    WormholeStatusOut(reachable = true, models = listOf(WormholeModelOut(name = "a", circuitState = state))),
                ),
                "circuitState=$state must not report failover",
            )
        }
    }

    @Test
    fun anUnknownRouterIsNotReported() {
        assertNull(wormholeSectionStatus(null))
    }
}
