package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.model.Airline
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsItem
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Outcome
import skytycoon.core.model.Plane
import skytycoon.core.model.QuarterResult
import skytycoon.core.model.Route
import skytycoon.core.model.RouteResult

object TurnEngine {

    /** 한 분기를 진행한다. 플레이어가 이번 분기에 내린 지시는 이미 state 에 반영돼 있어야 한다. */
    fun advance(state: GameState): GameState {
        if (state.outcome != null) return state
        val rng = Rng(state.rngState)
        var s = state

        s = Ai.actAll(s, rng)
        s = Events.stepWorld(s, rng)
        s = Events.fire(s, rng)

        val outcomes = Market.resolveAll(s)
        s = settle(s, outcomes)

        s = deliverOrders(s)
        s = ageFleet(s)
        s = updateBrandAndSafety(s)

        s = Stock.repriceAll(s)
        s = Stock.settleTakeovers(s)
        s = resolveDistress(s)

        s = s.copy(turn = s.turn + 1, rngState = rng.state)
        return checkEnd(s)
    }

    // --------------------------------------------------------------- 결산

    private fun settle(state: GameState, outcomes: Map<Int, RouteOutcome>): GameState {
        var s = state
        val routeResults = HashMap<Int, RouteResult>()

        for (airline in state.livingAirlines) {
            val planes = state.planesOf(airline.id)
            val routes = state.routesOf(airline.id)

            var pax = 0.0
            var seats = 0.0
            var rpk = 0.0
            var asks = 0.0
            var passengerRevenue = 0.0
            var cargoRevenue = 0.0
            var cost = RouteCost()

            for (route in routes) {
                val outcome = outcomes[route.id]
                val onRoute = planes.filter { it.routeId == route.id }
                if (outcome == null || onRoute.isEmpty()) {
                    routeResults[route.id] = RouteResult()
                    continue
                }
                val dist = Geo.distance(route.from, route.to)
                val cap = Economics.capacity(onRoute, dist)
                val effective = route.copy(freq = route.freq.coerceAtMost(cap.maxFreq))

                val cargo = outcome.revenue * Balance.CARGO_RATE
                val gross = outcome.revenue + cargo
                val rc = Economics.routeCost(s, airline, effective, onRoute, outcome.pax, gross)

                passengerRevenue += outcome.revenue
                cargoRevenue += cargo
                cost += rc
                pax += outcome.pax
                seats += outcome.seats
                rpk += outcome.pax * dist
                asks += outcome.seats * dist

                routeResults[route.id] = RouteResult(
                    pax = outcome.pax,
                    seats = outcome.seats,
                    revenue = gross,
                    cost = rc.total,
                    share = outcome.share,
                )
            }

            val overhead = Economics.overhead(s, airline)
            val depreciation = Economics.depreciation(planes)
            val businessIncome = airline.businesses.sumOf { it.type.income } * s.world.inflation
            val adWanted = airline.adBudget.values.sum()
            val adSpend = adWanted.coerceAtMost(maxOf(0.0, airline.cash) * 0.25)
            val interestCost = airline.debt * Actions.interestRate(s, airline) / 4.0

            val revenue = passengerRevenue + cargoRevenue + businessIncome
            val pretax = revenue - cost.total - overhead - depreciation - adSpend - interestCost
            val tax = if (pretax > 0) pretax * Balance.TAX_RATE else 0.0
            val net = pretax - tax
            // 감가상각은 현금이 나가지 않는다.
            val cashFlow = net + depreciation

            val equityAfter = Economics.equity(s, airline.copy(cash = airline.cash + cashFlow))
            val result = QuarterResult(
                turn = s.turn,
                revenue = passengerRevenue,
                cargoRevenue = cargoRevenue,
                fuelCost = cost.fuel,
                crewCost = cost.crew,
                maintCost = cost.maint,
                landingCost = cost.landing + cost.nav,
                paxServiceCost = cost.paxService,
                distributionCost = cost.distribution,
                overhead = overhead,
                adSpend = adSpend,
                depreciation = depreciation,
                interestCost = interestCost,
                businessIncome = businessIncome,
                tax = tax,
                net = net,
                pax = pax,
                rpk = rpk,
                asks = asks,
                cash = airline.cash + cashFlow,
                debt = airline.debt,
                equity = equityAfter,
            )

            s = s.withAirline(airline.id) {
                it.copy(
                    cash = it.cash + cashFlow,
                    results = (it.results + result).takeLast(80),
                    negativeQuarters = if (equityAfter < 0) it.negativeQuarters + 1 else 0,
                )
            }
        }

        return s.copy(
            routes = s.routes.map { r -> routeResults[r.id]?.let { r.copy(last = it) } ?: r },
        )
    }

    // --------------------------------------------------------------- 기재·브랜드

    private fun deliverOrders(state: GameState): GameState {
        val due = state.orders.filter { it.deliverTurn <= state.turn }
        if (due.isEmpty()) return state
        var nextId = state.nextId
        val newPlanes = mutableListOf<Plane>()
        val news = mutableListOf<NewsItem>()
        for (o in due) {
            repeat(o.count) {
                newPlanes += Plane(id = nextId++, typeId = o.typeId, airlineId = o.airlineId, ageQuarters = 0)
            }
            val airline = state.airlineOrNull(o.airlineId)
            if (airline?.isPlayer == true) {
                news += NewsItem(
                    state.turn,
                    NewsKind.PLAYER,
                    "${AircraftCatalog[o.typeId].name} ${o.count}대 인도 완료",
                    "새 기재가 도착했습니다. 노선에 배속하세요.",
                )
            }
        }
        return state.copy(
            planes = state.planes + newPlanes,
            orders = state.orders - due.toSet(),
            nextId = nextId,
            news = state.news + news,
        )
    }

    private fun ageFleet(state: GameState): GameState =
        state.copy(planes = state.planes.map { it.copy(ageQuarters = it.ageQuarters + 1) })

    private fun updateBrandAndSafety(state: GameState): GameState = state.copy(
        airlines = state.airlines.map { a ->
            if (!a.alive) {
                a
            } else {
                val spent = a.adBudget.values.sum().coerceAtMost(maxOf(0.0, a.cash) * 0.25 + 1.0)
                val totalWanted = a.adBudget.values.sum().coerceAtLeast(1.0)
                val brand = a.brand.mapValues { (region, v) ->
                    val regionSpend = (a.adBudget[region] ?: 0.0) * (spent / totalWanted)
                    val gain = regionSpend / 1e6 * Balance.AD_EFFICIENCY * state.world.inflation.let { 1.0 / it }
                    (v * Balance.BRAND_DECAY + gain).coerceIn(0.0, Balance.BRAND_MAX)
                }
                a.copy(brand = brand, safety = (a.safety + 0.015).coerceAtMost(1.0))
            }
        },
    )

    // --------------------------------------------------------------- 재무 위기

    /**
     * 현금이 마르면 차입 → 유휴 기재 매각 → 긴급 대출 순으로 막는다.
     * 자기자본이 [Balance.BANKRUPTCY_GRACE] 분기 연속 마이너스면 파산.
     */
    private fun resolveDistress(state: GameState): GameState {
        var s = state
        for (airline in state.livingAirlines) {
            var a = s.airline(airline.id)
            if (a.cash < 0) {
                val cap = Actions.debtCapacity(s, a)
                val room = (cap - a.debt).coerceAtLeast(0.0)
                val borrow = minOf(-a.cash, room)
                if (borrow > 0) {
                    a = a.copy(cash = a.cash + borrow, debt = a.debt + borrow)
                    s = s.withAirline(a.id) { a }
                }
            }
            if (a.cash < 0) {
                // 유휴 기재를 오래된 것부터 판다.
                val idle = s.planesOf(a.id).filter { it.routeId == null }
                    .sortedByDescending { it.ageQuarters }
                for (p in idle) {
                    if (a.cash >= 0) break
                    val proceeds = Actions.sellPrice(AircraftCatalog[p.typeId], p.ageQuarters)
                    a = a.copy(cash = a.cash + proceeds)
                    s = s.copy(planes = s.planes.filter { it.id != p.id }).withAirline(a.id) { a }
                }
            }
            if (a.cash < 0) {
                // 마지막 수단 — 고리 긴급 대출
                val need = -a.cash
                a = a.copy(cash = 0.0, debt = a.debt + need * 1.15)
                s = s.withAirline(a.id) { a }
                if (a.isPlayer) {
                    s = s.copy(
                        news = s.news + NewsItem(
                            s.turn,
                            NewsKind.PLAYER,
                            "긴급 자금 수혈",
                            "현금이 바닥나 고금리 긴급 대출을 받았습니다. 재무 구조를 손봐야 합니다.",
                        ),
                    )
                }
            }

            if (a.negativeQuarters >= Balance.BANKRUPTCY_GRACE) {
                s = bankrupt(s, a)
            }
        }
        return s
    }

    private fun bankrupt(state: GameState, airline: Airline): GameState {
        val s = state
            .copy(
                planes = state.planes.filter { it.airlineId != airline.id },
                routes = state.routes.filter { it.airlineId != airline.id },
            )
            .withAirline(airline.id) {
                it.copy(alive = false, cash = 0.0, debt = 0.0, slots = emptyMap(), holdings = emptyMap())
            }
            .copy(
                news = state.news + NewsItem(
                    state.turn,
                    NewsKind.MARKET,
                    "${airline.name} 파산",
                    "자본잠식이 이어져 운항을 중단했습니다. 슬롯이 시장에 풀립니다.",
                ),
            )
        return s
    }

    // --------------------------------------------------------------- 승패

    fun ranking(state: GameState): List<Pair<Airline, Double>> =
        state.airlines
            .filter { it.alive }
            .map { it to Economics.equity(state, it) }
            .sortedByDescending { it.second }

    private fun checkEnd(state: GameState): GameState {
        val player = state.airlineOrNull(state.playerId)
        if (player == null || !player.alive) {
            return state.copy(
                outcome = Outcome(false, state.airlines.size, "회사가 사라졌습니다."),
                news = state.news + NewsItem(state.turn, NewsKind.MILESTONE, "게임 오버", "경영에 실패했습니다."),
            )
        }
        val rivals = state.livingAirlines.filter { it.id != state.playerId }
        if (rivals.isEmpty()) {
            return state.copy(
                outcome = Outcome(true, 1, "모든 경쟁사를 흡수했습니다. 하늘은 당신의 것입니다."),
                news = state.news + NewsItem(state.turn, NewsKind.MILESTONE, "천하통일", "경쟁사가 모두 사라졌습니다."),
            )
        }
        if (state.turn < state.totalTurns) return state

        val rank = ranking(state)
        val idx = rank.indexOfFirst { it.first.id == state.playerId }
        val position = if (idx < 0) rank.size + 1 else idx + 1
        val won = position == 1
        return state.copy(
            outcome = Outcome(
                won = won,
                rank = position,
                reason = if (won) {
                    "기업가치 1위로 시대를 마감했습니다."
                } else {
                    "${position}위로 마쳤습니다. 다음엔 더 멀리 날 수 있습니다."
                },
            ),
            news = state.news + NewsItem(
                state.turn,
                NewsKind.MILESTONE,
                if (won) "왕좌에 오르다" else "시대의 끝",
                "최종 순위 ${position}위.",
            ),
        )
    }
}
