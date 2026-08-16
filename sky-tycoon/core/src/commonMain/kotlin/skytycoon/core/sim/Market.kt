package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.model.City
import skytycoon.core.model.GameState
import skytycoon.core.model.Route
import kotlin.math.exp
import kotlin.math.ln
import kotlin.math.pow

/** 한 노선이 이번 분기에 실제로 실어 나른 결과. */
data class RouteOutcome(
    val routeId: Int,
    val bizPax: Double,
    val leiPax: Double,
    val seats: Double,
    val fare: Double,
    val revenue: Double,
    val share: Double,
) {
    val pax: Double get() = bizPax + leiPax
}

private class Offer(
    val route: Route,
    val fare: Double,
    val seats: Double,
    val bizUtil: Double,
    val leiUtil: Double,
) {
    var remaining: Double = seats
    var biz: Double = 0.0
    var lei: Double = 0.0
}

object Market {
    /**
     * 도시쌍 하나의 승객 배분.
     *
     * 비즈니스 승객이 먼저 좌석을 가져가고(수익 관리), 남은 좌석을 레저 승객이 채운다.
     * 각 세그먼트 안에서는 다항 로짓으로 점유율이 갈리며, 좌석이 모자라 넘친 수요는
     * 여유가 있는 다른 항공사로 몇 차례에 걸쳐 흘러간다.
     */
    fun resolvePair(state: GameState, a: City, b: City, routes: List<Route>): List<RouteOutcome> {
        if (routes.isEmpty()) return emptyList()
        // 공항이 닫히면 비행기가 안 뜬다. 빈 결과를 돌려줘야 결산에서 연료·승무원·착륙료가
        // 청구되지 않는다 (수요만 0 으로 만들면 원가는 그대로 나간다).
        if (state.cityState[a.id]?.isClosed(state.turn) == true ||
            state.cityState[b.id]?.isClosed(state.turn) == true
        ) {
            return emptyList()
        }
        val dist = Geo.distance(a.id, b.id)
        val standard = Economics.standardFare(dist, state.world.inflation)

        val offers = ArrayList<Offer>(routes.size)
        for (r in routes) {
            if (!r.active || r.freq <= 0 || r.planeIds.isEmpty()) continue
            val airline = state.airlineOrNull(r.airlineId) ?: continue
            if (!airline.alive) continue
            val planes = r.planeIds.mapNotNull { id -> state.planes.firstOrNull { it.id == id } }
            if (planes.isEmpty()) continue
            val cap = Economics.capacity(planes, dist)
            if (!cap.usable) continue
            val freq = r.freq.coerceAtMost(cap.maxFreq)
            if (freq <= 0) continue

            val seats = Economics.quarterlySeats(freq, cap.avgSeats)
            val fare = standard * r.fareMul
            val fareRatio = (fare / standard).coerceAtLeast(0.05)

            val brand = (airline.brandIn(a.region) + airline.brandIn(b.region)) / 2.0
            val hub = (
                airline.slotsAt(a.id).toDouble() / a.slots.coerceAtLeast(1) +
                    airline.slotsAt(b.id).toDouble() / b.slots.coerceAtLeast(1)
                ) / 2.0
            val prestige = planes.sumOf { AircraftCatalog[it.typeId].prestige } / planes.size
            val bizFacilities = airline.businesses
                .filter { it.city == a.id || it.city == b.id }
                .sumOf { it.type.demandBoost }

            val common = Balance.SHARE_BRAND_W * brand +
                Balance.SHARE_PRESTIGE_W * prestige +
                Balance.SHARE_HUB_W * hub +
                Balance.SHARE_SAFETY_W * (airline.safety - 1.0) +
                bizFacilities

            val service = (airline.serviceLevel + r.serviceExtra).toDouble()
            val logFreq = ln(1.0 + freq.toDouble())
            val logFare = ln(fareRatio)

            offers += Offer(
                route = r,
                fare = fare,
                seats = seats,
                bizUtil = -Balance.BIZ_PRICE_SENS * logFare +
                    Balance.BIZ_SERVICE_W * service +
                    Balance.BIZ_FREQ_W * logFreq + common,
                leiUtil = -Balance.LEI_PRICE_SENS * logFare +
                    Balance.LEI_SERVICE_W * service +
                    Balance.LEI_FREQ_W * logFreq + common,
            )
        }
        if (offers.isEmpty()) return emptyList()

        val demand = Demand.quarterly(state, a, b)
        // 시장 평균 운임이 싸면 원래 안 움직였을 사람들까지 움직인다.
        val capacityTotal = offers.sumOf { it.seats }
        val weightedFareRatio = if (capacityTotal <= 0) {
            1.0
        } else {
            offers.sumOf { it.seats * (it.fare / standard) } / capacityTotal
        }
        val induced = weightedFareRatio.coerceIn(0.3, 3.0).pow(-Balance.INDUCED_ELASTICITY)

        allocate(offers, demand.business * induced) { it.bizUtil }
            .also { served -> offers.forEachIndexed { i, o -> o.biz = served[i] } }
        allocate(offers, demand.leisure * induced) { it.leiUtil }
            .also { served -> offers.forEachIndexed { i, o -> o.lei = served[i] } }

        val totalPax = offers.sumOf { it.biz + it.lei }
        return offers.map { o ->
            val revenue = o.biz * o.fare * Balance.BIZ_YIELD + o.lei * o.fare * Balance.LEI_YIELD
            RouteOutcome(
                routeId = o.route.id,
                bizPax = o.biz,
                leiPax = o.lei,
                seats = o.seats,
                fare = o.fare,
                revenue = revenue,
                share = if (totalPax <= 0) 0.0 else (o.biz + o.lei) / totalPax,
            )
        }
    }

    /**
     * 로짓 점유율로 수요를 나누고, 좌석이 모자라 넘친 몫을 여유 있는 곳으로 재배분한다.
     * [Offer.remaining] 을 소비하며 각 offer 가 실어 나른 인원 배열을 돌려준다.
     */
    private fun allocate(offers: List<Offer>, demand: Double, utility: (Offer) -> Double): DoubleArray {
        val taken = DoubleArray(offers.size)
        if (demand <= 0.0) return taken
        var left = demand

        repeat(Balance.SPILL_ROUNDS + 1) {
            if (left <= 1e-6) return@repeat
            val active = offers.indices.filter { offers[it].remaining > 1e-6 }
            if (active.isEmpty()) return@repeat

            val utils = active.map { utility(offers[it]) }
            val maxU = utils.max()
            val weights = utils.map { exp(it - maxU) }
            val sum = weights.sum()
            if (sum <= 0.0) return@repeat

            var served = 0.0
            for ((k, idx) in active.withIndex()) {
                val want = left * (weights[k] / sum)
                val o = offers[idx]
                val give = minOf(want, o.remaining)
                o.remaining -= give
                taken[idx] += give
                served += give
            }
            if (served <= 1e-9) return@repeat
            left -= served
        }
        return taken
    }

    /** 도시쌍 단위로 모든 항공사의 노선을 모아 한 번에 푼다. */
    fun resolveAll(state: GameState): Map<Int, RouteOutcome> {
        val byPair = HashMap<String, MutableList<Route>>()
        for (r in state.routes) {
            if (!r.active || r.freq <= 0 || r.planeIds.isEmpty()) continue
            byPair.getOrPut(Geo.pairKey(r.from, r.to)) { mutableListOf() }.add(r)
        }
        val out = HashMap<Int, RouteOutcome>()
        for ((key, routes) in byPair) {
            val (idA, idB) = key.split("|")
            for (outcome in resolvePair(state, Cities[idA], Cities[idB], routes)) {
                out[outcome.routeId] = outcome
            }
        }
        return out
    }
}
