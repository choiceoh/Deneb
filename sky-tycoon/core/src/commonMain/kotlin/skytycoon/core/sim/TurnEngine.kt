package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
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
        // AI 매집이 우리 회사를 삼켰다면 그 자리에서 판이 끝난다. 그대로 밀고 나가면
        // 이미 사라진 회사로 한 분기를 더 굴려, 결과 화면과 자동 저장이 인수 다음 분기로
        // 뜨는데 게임 오버 뉴스만 인수 분기에 찍힌다.
        if (s.outcome != null) return s.copy(rngState = rng.state)

        s = Events.stepWorld(s, rng)
        s = Events.fire(s, rng)

        // 결산 전에 판다 — 이번 분기에 취항을 접은 도시의 사업이 수익을 한 번 더 받으면 안 된다.
        s = liquidateOrphanBusinesses(s)

        val outcomes = Market.resolveAll(s)
        s = settle(s, outcomes)

        s = ageFleet(s)
        s = deliverOrders(s)
        s = deliverExpansions(s)
        s = updateBrandAndSafety(s)

        s = Stock.repriceAll(s)
        s = Stock.settleTakeovers(s)
        s = resolveDistress(s)
        // 합병·긴급 차입·기재 매각으로 대차대조표가 또 바뀌었다. 여기서 다시 매기지 않으면
        // 다음 분기 내내 낡은 주가로 지분을 사고팔게 된다.
        s = Stock.repriceAll(s)
        s = syncClosingBalances(s)

        s = clearQuarterlyCounters(s)
        // 해가 바뀌는 경계에서 물가·성장을 올린다 — 새해 첫 화면부터 새 가격이 보이도록.
        // 단, 마지막 분기를 넘기는 순간에는 하지 않는다 (플레이하지도 않을 해의 성장이
        // 최종 기업가치 순위에 얹히고, 결과 화면이 다음 해 1분기로 표시된다).
        if (s.turn + 1 < s.totalTurns) s = Events.yearTick(s, s.turn + 1)
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

            val extraordinary = airline.pendingCharges
            val overhead = Economics.overhead(s, airline)
            val depreciation = Economics.depreciation(planes)
            val businessIncome = airline.businesses.sumOf { it.type.income } * s.world.inflation
            val adWanted = airline.adBudget.values.sum()
            val adSpend = adWanted.coerceAtMost(maxOf(0.0, airline.cash) * 0.25)
            val interestCost = airline.debt * Actions.interestRate(s, airline) / 4.0

            val revenue = passengerRevenue + cargoRevenue + businessIncome
            val pretax = revenue - cost.total - overhead - depreciation - adSpend - interestCost - extraordinary
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
                extraordinaryCost = extraordinary,
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
                    pendingCharges = 0.0,
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
        // 죽은 회사 앞으로는 인도하지 않는다 (선금은 합병 때 인수사로 넘어간다).
        val alive = state.orders.filter { state.airlineOrNull(it.airlineId)?.alive == true }
        val dropped = state.orders - alive.toSet()
        // deliverTurn 은 "기재를 쓸 수 있게 되는 분기". 이 advance 가 끝나면 turn 이 +1 이다.
        val due = alive.filter { it.deliverTurn <= state.turn + 1 }
        if (due.isEmpty() && dropped.isEmpty()) return state
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
            orders = alive - due.toSet(),
            nextId = nextId,
            news = state.news + news,
        )
    }

    /** 공사가 끝난 공항에 슬롯을 풀고, 출자사 몫을 먼저 떼어 준다. */
    private fun deliverExpansions(state: GameState): GameState {
        val due = state.expansions.filter { it.deliverTurn <= state.turn + 1 }
        if (due.isEmpty()) return state
        var s = state.copy(expansions = state.expansions - due.toSet())
        for (e in due) {
            val city = Cities[e.city]
            val cs = s.cityState[e.city] ?: skytycoon.core.model.CityState()
            s = s.copy(
                cityState = s.cityState + (
                    e.city to cs.copy(extraSlots = cs.extraSlots + e.slots, expansions = cs.expansions + 1)
                    ),
            )
            // 출자사가 사라졌으면 그 몫도 그냥 시장에 풀린다.
            val sponsor = s.airlineOrNull(e.sponsorId)
            if (sponsor?.alive == true) {
                s = s.withAirline(e.sponsorId) {
                    it.copy(slots = it.slots + (e.city to it.slotsAt(e.city) + e.sponsorSlots))
                }
            }
            s = s.copy(
                news = s.news + NewsItem(
                    turn = s.turn,
                    kind = NewsKind.MARKET,
                    headline = "${city.name} 공항 확장 완공",
                    body = "슬롯 ${e.slots}개가 늘었습니다." +
                        if (sponsor?.alive == true) " ${sponsor.name}이 ${e.sponsorSlots}개를 배정받았습니다." else "",
                ),
            )
        }
        return s
    }

    /**
     * 긴급 차입·기재 매각으로 잔고가 바뀐 뒤에 결산 리포트의 기말 수치를 맞춰 준다.
     * 안 맞추면 리포트가 "기말 현금"이라며 구조 조치 이전 금액을 보여준다.
     */
    private fun syncClosingBalances(state: GameState): GameState = state.copy(
        airlines = state.airlines.map { a ->
            val last = a.results.lastOrNull() ?: return@map a
            if (last.turn != state.turn) return@map a
            val synced = last.copy(
                cash = a.cash,
                debt = a.debt,
                equity = Economics.equity(state, a),
            )
            a.copy(results = a.results.dropLast(1) + synced)
        },
    )

    /** 분기 상한(지분 매집·유상증자)은 분기가 넘어갈 때 비워진다. */
    private fun clearQuarterlyCounters(state: GameState): GameState = state.copy(
        airlines = state.airlines.map {
            if (it.boughtThisQuarter.isEmpty() && it.issuedThisQuarter == 0.0) {
                it
            } else {
                it.copy(boughtThisQuarter = emptyMap(), issuedThisQuarter = 0.0)
            }
        },
    )

    /**
     * 취항이 끊긴 도시의 부대사업을 장부가로 처분한다.
     *
     * `BuildBusiness` 는 "취항 중인 노선이 있는 도시"만 허용하는데, 그 조건이 깨지는 경로는
     * 셋이다 — 노선 폐지, 편수를 0 으로 내리기, 기재를 빼서 편수가 0 이 되기. 명령 쪽에서
     * 하나씩 막으면 반드시 하나를 빠뜨리므로(그동안 여러 번 그랬다) 전부 모이는 여기서 쓸어낸다.
     * 그러지 않으면 잠깐 노선을 열어 호텔을 짓고 바로 접어도 수익·자산·브랜드가 그대로 남는다.
     *
     * 처분가는 [Economics.businessValue] 와 같은 기준이라 자기자본은 그대로다 — 없어지는 건
     * 자격 없이 받던 **수익**뿐이다.
     */
    private fun liquidateOrphanBusinesses(state: GameState): GameState {
        var s = state
        for (airline in state.livingAirlines) {
            val current = s.airlineOrNull(airline.id) ?: continue
            if (current.businesses.isEmpty()) continue
            val served = s.routesOf(current.id).filter { it.active }
                .flatMap { listOf(it.from, it.to) }.toSet()
            val orphans = current.businesses.filter { it.city !in served }
            if (orphans.isEmpty()) continue

            val proceeds = Economics.businessValue(s, orphans)
            s = s.withAirline(current.id) {
                it.copy(cash = it.cash + proceeds, businesses = it.businesses.filter { b -> b.city in served })
            }
            if (current.isPlayer) {
                val where = orphans.map { Cities.name(it.city) }.distinct().joinToString(", ")
                s = s.copy(
                    news = s.news + NewsItem(
                        turn = s.turn,
                        kind = NewsKind.PLAYER,
                        headline = "취항이 끊긴 ${where}의 부대사업을 정리했습니다",
                        body = "${orphans.joinToString(", ") { it.type.label }} — 운영권은 취항 중인 도시에만 유지됩니다. " +
                            "장부가 ${(proceeds / 1e6).toInt()}백만 달러를 회수했습니다.",
                    ),
                )
            }
        }
        return s
    }

    private fun ageFleet(state: GameState): GameState =
        state.copy(planes = state.planes.map { it.copy(ageQuarters = it.ageQuarters + 1) })

    private fun updateBrandAndSafety(state: GameState): GameState = state.copy(
        airlines = state.airlines.map { a ->
            if (!a.alive) {
                a
            } else {
                // 결산에서 "실제로 청구된" 광고비를 그대로 쓴다. 여기서 다시 추정하면
                // 청구액과 브랜드 반영액이 어긋난다.
                val spent = a.lastResult?.adSpend ?: 0.0
                val totalWanted = a.adBudget.values.sum()
                val brand = a.brand.mapValues { (region, v) ->
                    val regionShare = if (totalWanted <= 0.0) 0.0 else (a.adBudget[region] ?: 0.0) / totalWanted
                    val gain = spent * regionShare / 1e6 * Balance.AD_EFFICIENCY / state.world.inflation
                    // 그 지역 도시에 낸 부대사업도 이름값을 만든다.
                    val facilities = a.businesses
                        .filter { b -> Cities[b.city].region == region }
                        .sumOf { b -> b.type.brandBoost }
                    (v * Balance.BRAND_DECAY + gain + facilities).coerceIn(0.0, Balance.BRAND_MAX)
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
                // 선금 치른 발주는 함께 사라진다. 안 지우면 죽은 회사 앞으로 기재가 계속 도착한다.
                orders = state.orders.filter { it.airlineId != airline.id },
                // 남의 손에 있던 이 회사 주식은 휴지가 된다. 그대로 두면 기업가치가 부풀려진다.
                airlines = state.airlines.map { a ->
                    if (a.holdings.containsKey(airline.id)) a.copy(holdings = a.holdings - airline.id) else a
                },
            )
            .withAirline(airline.id) {
                // 부대사업까지 정리해야 한다. 남겨두면 Economics.equity 가 원가의 70% 를
                // 계속 자산으로 쳐서, 파산한 회사가 기업가치 플러스로 순위표에 남는다.
                it.copy(
                    alive = false,
                    cash = 0.0,
                    debt = 0.0,
                    slots = emptyMap(),
                    holdings = emptyMap(),
                    businesses = emptyList(),
                )
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

    /** 플레이어 행동(지분 인수 등)으로 판이 끝났는지 즉시 확인할 때 쓴다. */
    fun evaluateOutcome(state: GameState): GameState =
        if (state.outcome != null) state else checkEnd(state)

    private fun checkEnd(state: GameState): GameState {
        val player = state.airlineOrNull(state.playerId)
        if (player == null || !player.alive) {
            // 살아남은 회사들 바로 뒤가 우리 자리다. 전체 회사 수를 쓰면 이미 망한
            // 경쟁사까지 우리보다 위로 세어져, 1:1 승부에서 져도 4위로 적힌다.
            return state.copy(
                outcome = Outcome(false, state.livingAirlines.size + 1, "회사가 사라졌습니다."),
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
            // 기한 종료로 오는 판정이라 turn 은 이미 totalTurns 다. 그대로 찍으면
            // 뉴스 화면이 플레이하지도 않은 다음 해 1분기에 최종 발표를 올린다.
            // (displayTurn 은 outcome 이 아직 안 붙은 이 시점에 보정해 주지 못한다.)
            news = state.news + NewsItem(
                (state.turn - 1).coerceAtLeast(0),
                NewsKind.MILESTONE,
                if (won) "왕좌에 오르다" else "시대의 끝",
                "최종 순위 ${position}위.",
            ),
        )
    }
}
