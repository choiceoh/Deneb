package skytycoon.core

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.model.GameState
import skytycoon.core.model.Plane
import skytycoon.core.model.Route
import skytycoon.core.sim.Balance
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Geo
import skytycoon.core.sim.Market
import skytycoon.core.sim.NewGame
import kotlin.test.Test
import kotlin.test.assertTrue

/**
 * 게임의 재미는 "노선 하나가 얼마나 벌어야 하는가"에 달려 있다.
 * 독점 노선은 확실히 남아야 확장할 맛이 나고, 경쟁이 붙으면 마진이 깎여야 긴장이 생긴다.
 */
class BalanceTest {

    private fun blankWorld(): GameState {
        val base = NewGame.create(seed = 7)
        return base.copy(
            routes = emptyList(),
            planes = base.planes.map { it.copy(routeId = null) },
        )
    }

    /** 지정한 항공사들이 같은 구간에 각각 노선을 하나씩 연 상태를 만든다. */
    private fun withCarriers(
        from: String,
        to: String,
        typeId: String,
        freq: Int,
        fareMul: Double,
        airlineIds: List<String>,
    ): GameState {
        var s = blankWorld()
        var nextId = s.nextId
        val planes = mutableListOf<Plane>()
        val routes = mutableListOf<Route>()
        for (id in airlineIds) {
            val planeId = nextId++
            val routeId = nextId++
            planes += Plane(planeId, typeId, id, ageQuarters = 8, routeId = routeId)
            routes += Route(
                id = routeId,
                airlineId = id,
                from = from,
                to = to,
                fareMul = fareMul,
                freq = freq,
                planeIds = listOf(planeId),
            )
        }
        // 슬롯도 맞춰준다 (밸런스 측정이 목적이라 슬롯 제약은 논외).
        s = s.copy(
            planes = planes,
            routes = routes,
            nextId = nextId,
            airlines = s.airlines.map { a ->
                if (a.id in airlineIds) {
                    a.copy(slots = a.slots + (from to 40) + (to to 40))
                } else {
                    a
                }
            },
        )
        return s
    }

    private data class Economy(val revenue: Double, val cost: Double, val pax: Double, val lf: Double) {
        val margin: Double get() = if (revenue <= 0) 0.0 else (revenue - cost) / revenue
    }

    private fun measure(state: GameState, routeId: Int): Economy {
        val route = state.route(routeId)
        val airline = state.airline(route.airlineId)
        val a = Cities[route.from]
        val b = Cities[route.to]
        val sameRoutes = state.routes.filter { Geo.pairKey(it.from, it.to) == Geo.pairKey(a.id, b.id) }
        val outcome = Market.resolvePair(state, a, b, sameRoutes).first { it.routeId == routeId }
        val planes = state.planes.filter { it.routeId == routeId }

        val cargo = outcome.revenue * Balance.CARGO_RATE
        val gross = outcome.revenue + cargo
        val direct = Economics.routeCost(state, airline, route, planes, outcome.pax, gross).total
        val ownership = Economics.depreciation(planes)
        val overheadShare = planes.size * Balance.OVERHEAD_PER_AIRCRAFT * state.world.inflation +
            Balance.OVERHEAD_PER_ROUTE * state.world.inflation
        return Economy(
            revenue = gross,
            cost = direct + ownership + overheadShare,
            pax = outcome.pax,
            lf = outcome.seats.let { if (it <= 0) 0.0 else outcome.pax / it },
        )
    }

    @Test
    fun `독점 노선은 확실히 남는다`() {
        val s = withCarriers("seoul", "tokyo", "b727", freq = 14, fareMul = 1.0, airlineIds = listOf("hanseong"))
        val routeId = s.routes.first().id
        val e = measure(s, routeId)
        println(
            "[독점 서울-도쿄] 승객 ${e.pax.toInt()}명, 탑승률 ${(e.lf * 100).toInt()}%, " +
                "매출 ${(e.revenue / 1e6)}M, 원가 ${(e.cost / 1e6)}M, 마진 ${(e.margin * 100).toInt()}%",
        )
        assertTrue(e.lf > 0.7, "독점인데 탑승률이 ${e.lf} 밖에 안 된다")
        assertTrue(e.margin in 0.12..0.55, "독점 노선 마진 ${e.margin} 이 기대 범위를 벗어났다")
    }

    @Test
    fun `경쟁이 붙으면 마진이 깎인다`() {
        val mono = withCarriers("seoul", "tokyo", "b727", 14, 1.0, listOf("hanseong"))
        val duo = withCarriers("seoul", "tokyo", "b727", 14, 1.0, listOf("hanseong", "fuji", "britannia"))
        val monoE = measure(mono, mono.routes.first().id)
        val duoE = measure(duo, duo.routes.first { it.airlineId == "hanseong" }.id)
        println(
            "[경쟁 3사] 탑승률 ${(duoE.lf * 100).toInt()}%, 마진 ${(duoE.margin * 100).toInt()}% " +
                "(독점 대비 ${((duoE.margin - monoE.margin) * 100).toInt()}%p)",
        )
        assertTrue(duoE.margin < monoE.margin, "경쟁이 붙었는데 마진이 줄지 않았다")
        assertTrue(duoE.pax < monoE.pax, "경쟁이 붙었는데 승객이 줄지 않았다")
        assertTrue(duoE.lf < 0.95, "경쟁 노선인데 여전히 만석이면 운임 경쟁이 성립하지 않는다")
    }

    /**
     * 좌석이 모자란 독점 노선에서는 값을 올려도 승객이 그대로다(어차피 만석).
     * 운임이 의미를 갖는 건 경쟁이 붙어 좌석에 여유가 생겼을 때다.
     */
    @Test
    fun `경쟁 시장에서 운임을 올리면 승객을 뺏긴다`() {
        fun paxAt(fare: Double): Double {
            var s = withCarriers("seoul", "tokyo", "b727", 14, 1.0, listOf("hanseong", "fuji", "britannia"))
            val mine = s.routes.first { it.airlineId == "hanseong" }
            s = s.copy(routes = s.routes.map { if (it.id == mine.id) it.copy(fareMul = fare) else it })
            return measure(s, mine.id).pax
        }
        val cheap = paxAt(0.75)
        val dear = paxAt(1.50)
        println("[운임] 0.75배 → ${cheap.toInt()}명 / 1.50배 → ${dear.toInt()}명")
        assertTrue(cheap > dear * 1.2, "운임을 두 배 가까이 올렸는데 승객이 거의 안 줄었다")
    }

    @Test
    fun `장거리 대형기 노선도 채산이 맞는다`() {
        val s = withCarriers("tokyo", "losangeles", "b747_100", freq = 7, fareMul = 1.0, listOf("fuji"))
        val e = measure(s, s.routes.first().id)
        println(
            "[도쿄-LA 747] 승객 ${e.pax.toInt()}명, 탑승률 ${(e.lf * 100).toInt()}%, " +
                "마진 ${(e.margin * 100).toInt()}%",
        )
        assertTrue(e.margin > -0.1, "장거리 간판 노선이 구조적 적자면 곤란하다 (마진 ${e.margin})")
    }

    @Test
    fun `기재 한 대의 주간 편수 상한이 상식적이다`() {
        val b727 = AircraftCatalog["b727"]
        val short = Economics.capacity(listOf(Plane(1, "b727", "x", 0)), Geo.distance("seoul", "tokyo"))
        val long = Economics.capacity(listOf(Plane(1, "b747_100", "x", 0)), Geo.distance("tokyo", "losangeles"))
        println("[가동] ${b727.name} 서울-도쿄 주 ${short.maxFreq}왕복 / B747 도쿄-LA 주 ${long.maxFreq}왕복")
        assertTrue(short.maxFreq in 12..30, "단거리 편수 상한 ${short.maxFreq} 이 이상하다")
        assertTrue(long.maxFreq in 2..6, "장거리 편수 상한 ${long.maxFreq} 이 이상하다")
    }
}
