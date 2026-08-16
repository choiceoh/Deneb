package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.data.Difficulties
import skytycoon.core.model.Airline
import skytycoon.core.model.AircraftType
import skytycoon.core.model.BusinessType
import skytycoon.core.model.City
import skytycoon.core.model.GameState
import skytycoon.core.model.Plane
import skytycoon.core.model.Region
import skytycoon.core.model.Route
import skytycoon.core.model.Trait

/**
 * 경쟁사 두뇌. 플레이어와 똑같이 [Actions] 를 통해서만 세계를 바꾸므로
 * AI 가 규칙을 우회하는 일은 구조적으로 생기지 않는다.
 */
object Ai {

    /**
     * @param includePlayer 플레이어 회사까지 AI 두뇌에 맡긴다 (자동조종 · 밸런스 검증용).
     */
    fun actAll(state: GameState, rng: Rng, includePlayer: Boolean = false): GameState {
        var s = state
        for (airline in state.livingAirlines.filter { includePlayer || !it.isPlayer }) {
            if (s.airlineOrNull(airline.id)?.alive != true) continue
            s = act(s, airline.id, rng)
        }
        return s
    }

    /** 플레이어 회사 하나만 AI 에게 맡긴다. */
    fun autoPilot(state: GameState, airlineId: String, rng: Rng): GameState =
        if (state.airlineOrNull(airlineId)?.alive == true) act(state, airlineId, rng) else state

    private fun act(state: GameState, airlineId: String, rng: Rng): GameState {
        var s = state
        val skill = Difficulties[s.difficultyId].aiSkill

        s = tuneRoutes(s, airlineId, rng, skill)
        s = pruneRoutes(s, airlineId, rng)
        s = growFrequency(s, airlineId)
        s = upgauge(s, airlineId, skill)
        s = manageFleet(s, airlineId, rng, skill)
        s = manageSlots(s, airlineId, rng, skill)
        s = openRoutes(s, airlineId, rng, skill)
        s = finance(s, airlineId)
        s = marketing(s, airlineId, skill)
        s = sideBusiness(s, airlineId, rng)
        s = stockMoves(s, airlineId, rng, skill)
        return s
    }

    private fun cmd(state: GameState, c: Command): GameState = Actions.execute(state, c).state

    // ------------------------------------------------------------- 운임·편수 조정

    private fun targetFare(trait: Trait): Double = when (trait) {
        Trait.VALUE -> 0.86
        Trait.PREMIUM -> 1.18
        Trait.EXPAND -> 0.96
        Trait.BALANCED -> 1.0
    }

    private fun tuneRoutes(state: GameState, airlineId: String, rng: Rng, skill: Double): GameState {
        var s = state
        val airline = s.airline(airlineId)
        val anchor = targetFare(airline.trait)
        for (route in s.routesOf(airlineId)) {
            val last = route.last ?: continue
            val lf = last.loadFactor
            var fare = route.fareMul
            // 만석이면 값을 올리고, 텅 비면 내린다. 실력이 낮은 AI 는 반응이 굼뜨고 잡음이 섞인다.
            fare *= when {
                lf > 0.90 -> 1.0 + 0.05 * skill
                lf > 0.82 -> 1.0 + 0.02 * skill
                lf < 0.50 -> 1.0 - 0.06 * skill
                lf < 0.65 -> 1.0 - 0.03 * skill
                else -> 1.0
            }
            fare += rng.range(-0.02, 0.02) * (2.0 - skill)
            // 회사 성향이 정한 기준선으로 서서히 끌려간다.
            fare = fare * 0.85 + anchor * 0.15
            s = cmd(s, Command.TuneRoute(airlineId, route.id, fareMul = fare.coerceIn(0.6, 1.7)))
        }
        return s
    }

    /** 돈이 안 되고 손님도 없는 노선은 접는다. */
    private fun pruneRoutes(state: GameState, airlineId: String, rng: Rng): GameState {
        var s = state
        for (route in s.routesOf(airlineId)) {
            val last = route.last ?: continue
            val hopeless = last.profit < 0 && last.loadFactor < 0.48
            if (hopeless && rng.chance(0.45)) {
                s = cmd(s, Command.CloseRoute(airlineId, route.id))
            }
        }
        return s
    }

    /** 잘 나가는 노선에 편수를 붙이고, 남는 기재를 밀어 넣는다. */
    private fun growFrequency(state: GameState, airlineId: String): GameState {
        var s = state
        val ranked = s.routesOf(airlineId)
            .filter { (it.last?.loadFactor ?: 0.0) > 0.84 }
            .sortedByDescending { it.last?.profit ?: 0.0 }

        for (route in ranked) {
            val current = s.routes.firstOrNull { it.id == route.id } ?: continue
            val dist = Geo.distance(current.from, current.to)
            val onRoute = s.planes.filter { it.routeId == current.id }
            val cap = Economics.capacity(onRoute, dist)

            if (current.freq < cap.maxFreq) {
                s = cmd(s, Command.TuneRoute(airlineId, current.id, freq = current.freq + 1))
                continue
            }
            // 기재가 한계면 유휴기를 추가 투입한다.
            val idle = s.planesOf(airlineId).firstOrNull {
                it.routeId == null && Economics.canFly(AircraftCatalog[it.typeId], dist)
            } ?: continue
            s = cmd(s, Command.AssignPlanes(airlineId, current.id, current.planeIds + idle.id))
            val grown = s.routes.firstOrNull { it.id == current.id } ?: continue
            val newCap = Economics.capacity(s.planes.filter { it.routeId == grown.id }, dist)
            val want = minOf(
                newCap.maxFreq,
                grown.freq + s.freeSlots(airlineId, grown.from).coerceAtLeast(0),
                grown.freq + s.freeSlots(airlineId, grown.to).coerceAtLeast(0),
            )
            if (want > grown.freq) {
                s = cmd(s, Command.TuneRoute(airlineId, grown.id, freq = want))
            }
        }
        return s
    }

    /**
     * 슬롯이 동나 편수를 못 늘리는데 만석인 노선은, 더 큰 기재로 갈아타는 수밖에 없다.
     * 이걸 안 하면 허브가 포화된 순간부터 회사가 현금만 쌓아두고 멈춰 선다.
     */
    private fun upgauge(state: GameState, airlineId: String, skill: Double): GameState {
        var s = state
        val stuck = s.routesOf(airlineId).filter { r ->
            val lf = r.last?.loadFactor ?: 0.0
            if (lf <= 0.88) return@filter false
            val slotRoom = minOf(s.freeSlots(airlineId, r.from), s.freeSlots(airlineId, r.to))
            val soldOut = s.unsoldSlots(Cities[r.from]) == 0 || s.unsoldSlots(Cities[r.to]) == 0
            slotRoom <= 0 && soldOut
        }.sortedByDescending { it.last?.pax ?: 0.0 }

        for (route in stuck.take(2)) {
            val current = s.routes.firstOrNull { it.id == route.id } ?: continue
            val dist = Geo.distance(current.from, current.to)
            val onRoute = s.planes.filter { it.routeId == current.id }
            if (onRoute.isEmpty()) continue
            val smallest = onRoute.minByOrNull { AircraftCatalog[it.typeId].seats } ?: continue
            val smallestSeats = AircraftCatalog[smallest.typeId].seats

            // 이미 가진 유휴기 중에 더 큰 게 있으면 바꿔 단다.
            val idleBigger = s.planesOf(airlineId)
                .filter {
                    it.routeId == null && Economics.canFly(AircraftCatalog[it.typeId], dist) &&
                        AircraftCatalog[it.typeId].seats > smallestSeats
                }
                .maxByOrNull { AircraftCatalog[it.typeId].seats }

            if (idleBigger != null) {
                val swapped = current.planeIds - smallest.id + idleBigger.id
                s = cmd(s, Command.AssignPlanes(airlineId, current.id, swapped))
                s = cmd(s, Command.SellAircraft(airlineId, smallest.id))
                continue
            }

            // 없으면 발주한다.
            val airline = s.airline(airlineId)
            val reserve = Balance.AI_CASH_FLOOR * s.world.inflation
            val bigger = AircraftCatalog.newFor(s.year)
                .filter { Economics.canFly(it, dist) && it.seats > smallestSeats * 1.25 }
                .filter { it.price < (airline.cash - reserve) * 0.5 * skill }
                .maxByOrNull { it.seats } ?: continue
            s = cmd(s, Command.BuyAircraft(airlineId, bigger.id, 1))
        }
        return s
    }

    // ------------------------------------------------------------- 기재

    /** 노선망 평균 거리에 맞는, 좌석당 가격이 가장 싼 기종을 고른다. */
    private fun preferredType(state: GameState, airline: Airline): AircraftType? {
        val routes = state.routesOf(airline.id)
        val typicalDist = if (routes.isEmpty()) {
            2500.0
        } else {
            routes.sumOf { Geo.distance(it.from, it.to) } / routes.size
        }
        val candidates = AircraftCatalog.newFor(state.year).filter { Economics.canFly(it, typicalDist) }
        if (candidates.isEmpty()) return null
        return when (airline.trait) {
            Trait.PREMIUM -> candidates.maxByOrNull { it.prestige + it.seats / 200.0 }
            Trait.VALUE -> candidates.minByOrNull { it.price / it.seats }
            else -> candidates.minByOrNull { (it.price / it.seats) * (1.0 + it.fuel / it.seats * 20) }
        }
    }

    private fun manageFleet(state: GameState, airlineId: String, rng: Rng, skill: Double): GameState {
        var s = state
        val airline = s.airline(airlineId)
        val planes = s.planesOf(airlineId)

        // 25년 넘은 기재는 정리한다.
        for (p in planes.filter { it.routeId == null && it.ageQuarters > 100 }) {
            s = cmd(s, Command.SellAircraft(airlineId, p.id))
        }

        val idle = s.planesOf(airlineId).count { it.routeId == null }
        if (idle >= 4) return s

        val type = preferredType(s, s.airline(airlineId)) ?: return s
        val aggression = when (airline.trait) {
            Trait.EXPAND -> 1.35
            Trait.VALUE -> 1.1
            Trait.PREMIUM -> 0.9
            Trait.BALANCED -> 1.0
        } * skill

        val cash = s.airline(airlineId).cash
        val affordable = ((cash - Balance.AI_CASH_FLOOR * s.world.inflation) / type.price).toInt()
        val want = (affordable.coerceAtMost(Balance.AI_MAX_ORDERS) * aggression).toInt()
        if (want >= 1) {
            s = cmd(s, Command.BuyAircraft(airlineId, type.id, want.coerceAtMost(Balance.AI_MAX_ORDERS)))
        } else if (affordable < 1 && rng.chance(0.3 * skill)) {
            // 신조기가 부담되면 중고를 노린다.
            val used = AircraftCatalog.usedFor(s.year)
                .filter { Economics.canFly(it, 2000.0) }
                .minByOrNull { it.price }
            if (used != null) s = cmd(s, Command.BuyAircraft(airlineId, used.id, 1, used = true))
        }
        return s
    }

    // ------------------------------------------------------------- 슬롯

    /** 허브에 슬롯 여유가 없으면 확장이 막히므로 미리 사둔다. */
    private fun manageSlots(state: GameState, airlineId: String, rng: Rng, skill: Double): GameState {
        var s = state
        val home = s.airline(airlineId).home
        if (s.freeSlots(airlineId, home) >= 6) return s
        val price = Economics.slotPrice(s, airlineId, home)
        val budget = (s.airline(airlineId).cash - Balance.AI_CASH_FLOOR * s.world.inflation) * 0.25 * skill
        val count = (budget / price).toInt().coerceIn(0, 8)
        if (count >= 1 && s.unsoldSlots(Cities[home]) >= count && rng.chance(0.85)) {
            s = cmd(s, Command.BuySlots(airlineId, home, count))
        }
        return s
    }

    // ------------------------------------------------------------- 신규 노선

    private class Candidate(
        val from: City,
        val to: City,
        val plane: Plane,
        val freq: Int,
        val slotsFrom: Int,
        val slotsTo: Int,
        val slotCost: Double,
        val score: Double,
    )

    /**
     * 취항 후보를 고를 때 **목적지 슬롯 매입비까지 포함해서** 판단한다.
     * 이걸 빼먹으면 AI 는 창업 때 받은 다섯 개 도시 밖으로 영영 나가지 못한다.
     */
    private fun openRoutes(state: GameState, airlineId: String, rng: Rng, skill: Double): GameState {
        var s = state
        var opened = 0
        val limit = (Balance.AI_MAX_NEW_ROUTES * skill).toInt().coerceAtLeast(1)

        while (opened < limit) {
            val airline = s.airline(airlineId)
            val idle = s.planesOf(airlineId).filter { it.routeId == null }
            if (idle.isEmpty()) break

            val reserve = Balance.AI_CASH_FLOOR * s.world.inflation
            val budget = (airline.cash - reserve) * 0.55 * skill
            if (budget <= 0) break

            val served = s.routesOf(airlineId).map { Geo.pairKey(it.from, it.to) }.toSet()
            // 슬롯을 이미 가진 도시가 출발 거점. 많이 가진 순으로 몇 곳만 본다.
            val origins = airline.slots.filterValues { it > 0 }.keys
                .sortedByDescending { airline.slotsAt(it) }
                .take(8)

            var best: Candidate? = null
            for (fromId in origins) {
                val from = Cities[fromId]
                if (s.cityState[fromId]?.isClosed(s.turn) == true) continue
                for (to in Cities.all) {
                    if (to.id == fromId) continue
                    if (Geo.pairKey(fromId, to.id) in served) continue
                    if (s.cityState[to.id]?.isClosed(s.turn) == true) continue

                    val dist = Geo.distance(fromId, to.id)
                    val plane = idle
                        .filter { Economics.canFly(AircraftCatalog[it.typeId], dist) }
                        .maxByOrNull { AircraftCatalog[it.typeId].seats } ?: continue

                    val cap = Economics.capacity(listOf(plane), dist)
                    if (cap.maxFreq < 1) continue
                    val freq = minOf(cap.maxFreq, 7)

                    val needFrom = (freq - s.freeSlots(airlineId, fromId)).coerceAtLeast(0)
                    val needTo = (freq - s.freeSlots(airlineId, to.id)).coerceAtLeast(0)
                    if (needFrom > s.unsoldSlots(from) || needTo > s.unsoldSlots(to)) continue

                    // 슬롯값은 한 개 살 때마다 오른다. 현재 단가에 개수를 곱하면 실제보다 싸게
                    // 잡혀, 예산은 통과했는데 BuySlots 가 실패한다 — 그때 반대편 슬롯은 이미
                    // 사둔 뒤라 노선도 못 열고 슬롯만 놀린다. 매입과 같은 계산을 쓴다.
                    val cost = Actions.slotCost(s, airlineId, fromId, needFrom) +
                        Actions.slotCost(s, airlineId, to.id, needTo) +
                        Actions.routeSetupCost(s, fromId, to.id)
                    if (cost > budget) continue

                    // 같은 매력이면 슬롯값이 싼 쪽이 낫다.
                    val score = attractiveness(s, airlineId, from, to) / (1.0 + cost / 60e6)
                    if (best == null || score > best.score) {
                        best = Candidate(from, to, plane, freq, needFrom, needTo, cost, score)
                    }
                }
            }
            val target = best ?: break
            if (target.score <= 0.0) break

            if (target.slotsFrom > 0) {
                s = cmd(s, Command.BuySlots(airlineId, target.from.id, target.slotsFrom))
            }
            if (target.slotsTo > 0) {
                s = cmd(s, Command.BuySlots(airlineId, target.to.id, target.slotsTo))
            }
            val freq = minOf(
                target.freq,
                s.freeSlots(airlineId, target.from.id),
                s.freeSlots(airlineId, target.to.id),
            )
            if (freq < 1) break

            val before = s.routes.size
            s = cmd(
                s,
                Command.OpenRoute(
                    airlineId = airlineId,
                    from = target.from.id,
                    to = target.to.id,
                    planeIds = listOf(target.plane.id),
                    freq = freq,
                    fareMul = targetFare(airline.trait) + rng.range(-0.04, 0.04),
                ),
            )
            if (s.routes.size == before) break
            opened++
        }
        return s
    }

    /** 수요가 크고 경쟁이 옅을수록 매력적이다. */
    private fun attractiveness(state: GameState, airlineId: String, a: City, b: City): Double {
        val demand = Demand.quarterly(state, a, b).total
        if (demand < 1500) return 0.0
        val rivalSeats = state.routes
            .filter { it.active && Geo.pairKey(it.from, it.to) == Geo.pairKey(a.id, b.id) }
            .sumOf { r ->
                val planes = state.planes.filter { it.routeId == r.id }
                val cap = Economics.capacity(planes, Geo.distance(a.id, b.id))
                Economics.quarterlySeats(r.freq.coerceAtMost(cap.maxFreq), cap.avgSeats)
            }
        val airline = state.airline(airlineId)
        val homeBonus = if (a.id == airline.home || b.id == airline.home) 1.35 else 1.0
        val brandBonus = 1.0 + (airline.brandIn(a.region) + airline.brandIn(b.region)) / 400.0
        return demand / (1.0 + rivalSeats / 1000.0) * homeBonus * brandBonus
    }

    // ------------------------------------------------------------- 재무·마케팅·부대사업

    private fun finance(state: GameState, airlineId: String): GameState {
        var s = state
        val a = s.airline(airlineId)
        val floor = Balance.AI_CASH_FLOOR * s.world.inflation
        if (a.cash < floor) {
            val room = (Actions.debtCapacity(s, a) - a.debt).coerceAtLeast(0.0)
            val need = (floor * 3 - a.cash).coerceAtMost(room)
            if (need > 1e6) s = cmd(s, Command.Loan(airlineId, need))
        } else if (a.debt > 0 && a.cash > floor * 8) {
            val repay = minOf(a.debt, a.cash - floor * 6)
            if (repay > 1e6) s = cmd(s, Command.Loan(airlineId, -repay))
        }
        return s
    }

    private fun marketing(state: GameState, airlineId: String, skill: Double): GameState {
        var s = state
        val a = s.airline(airlineId)
        val revenue = a.lastResult?.totalRevenue ?: 0.0
        val budget = revenue * 0.035 * skill
        val regions = s.routesOf(airlineId)
            .flatMap { listOf(Cities[it.from].region, Cities[it.to].region) }
            .distinct()
            .ifEmpty { listOf(Cities[a.home].region) }
        val each = if (regions.isEmpty()) 0.0 else budget / regions.size
        for (r in Region.entries) {
            val amount = if (r in regions) each else 0.0
            s = cmd(s, Command.SetAdBudget(airlineId, r, amount))
        }
        if (a.trait == Trait.PREMIUM && a.serviceLevel < 5 &&
            a.cash > Actions.serviceUpgradeCost(s, a) * 6
        ) {
            s = cmd(s, Command.UpgradeService(airlineId))
        }
        return s
    }

    private fun sideBusiness(state: GameState, airlineId: String, rng: Rng): GameState {
        var s = state
        val a = s.airline(airlineId)
        if (a.cash < 220e6 * s.world.inflation) return s
        if (!rng.chance(0.35)) return s

        // 취항 중인 도시에만 낼 수 있고, 정비창은 전사 하나뿐이다 (BuildBusiness 규칙과 맞춘다).
        val served = s.routesOf(airlineId)
            .filter { it.active }
            .flatMap { listOf(it.from, it.to) }
            .distinct()
        if (served.isEmpty()) return s
        val candidates = BusinessType.entries.filter { t ->
            t != BusinessType.HANGAR || a.businesses.none { it.type == BusinessType.HANGAR }
        }
        if (candidates.isEmpty()) return s
        val type = rng.pick(candidates)
        val city = served
            .filter { c -> a.businesses.none { it.type == type && it.city == c } }
            .maxByOrNull { a.slotsAt(it) } ?: return s
        return cmd(s, Command.BuildBusiness(airlineId, type, city))
    }

    /**
     * 지분 매집. 아무나 노리지 않는다 — 실적이 무너졌거나 자기보다 한참 작은 회사만 사냥한다.
     * 그리고 자기가 사냥당하는 중이면 먼저 유상증자로 방어한다.
     */
    private fun stockMoves(state: GameState, airlineId: String, rng: Rng, skill: Double): GameState {
        var s = defendOwnership(state, airlineId)
        val a = s.airline(airlineId)
        if (a.trait != Trait.EXPAND && !rng.chance(0.25)) return s

        val warChest = a.cash - 400e6 * s.world.inflation
        if (warChest <= 0) return s
        val myEquity = Economics.equity(s, a)

        val target = s.livingAirlines
            .filter { it.id != airlineId }
            .filter { rival ->
                val equity = Economics.equity(s, rival)
                val ailing = rival.results.takeLast(4).sumOf { r -> r.net } < 0
                // 휘청이는 회사이거나, 내가 세 배 넘게 크면 삼킬 만하다.
                ailing || myEquity > equity * 3.0
            }
            .minByOrNull { Economics.equity(s, it) } ?: return s

        val limit = Stock.maxBuyableThisQuarter(s, airlineId, target.id)
        if (limit <= 0) return s
        val budget = warChest * 0.4 * skill
        val unit = Stock.buyCost(s, airlineId, target.id, 1.0)
        if (unit <= 0) return s
        // 과반을 넘기는 물량이면 잔여 지분 정리 대금까지 감당돼야 통과한다. 예산만 보고
        // 크게 지르면 매 분기 거절당하며 49% 에 영영 머문다 — UI 버튼과 같은 계산으로 깎는다.
        val affordable = Stock.affordableShares(s, airlineId, target.id)
        val shares = (budget / unit).coerceAtMost(limit).coerceAtMost(affordable)
        if (shares < target.shares * 0.02) return s
        return cmd(s, Command.TradeShares(airlineId, target.id, shares))
    }

    /** 누군가 25% 넘게 들고 있으면 증자로 희석한다. */
    private fun defendOwnership(state: GameState, airlineId: String): GameState {
        val threat = state.livingAirlines
            .filter { it.id != airlineId }
            .maxOfOrNull { Stock.ownershipRatio(state, it.id, airlineId) } ?: 0.0
        if (threat < 0.25) return state
        val me = state.airline(airlineId)
        val issue = Actions.maxIssuable(me) * (if (threat > 0.4) 1.0 else 0.5)
        return cmd(state, Command.IssueShares(airlineId, issue))
    }
}
