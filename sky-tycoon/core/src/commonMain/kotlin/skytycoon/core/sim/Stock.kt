package skytycoon.core.sim

import skytycoon.core.model.Airline
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsItem
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Route

object Stock {
    /** 주가 = 자산가치와 수익력의 혼합. 적자가 이어지면 주가가 무너져 인수당하기 쉬워진다. */
    fun price(state: GameState, airline: Airline): Double {
        val equity = Economics.equity(state, airline)
        val annualNet = airline.results.takeLast(4).sumOf { it.net }
        val earningsValue = annualNet * Balance.SHARE_PE
        val blended = Balance.SHARE_ASSET_W * equity + (1 - Balance.SHARE_ASSET_W) * earningsValue
        val floor = equity * 0.25
        return (maxOf(blended, floor) / airline.shares.coerceAtLeast(1.0))
            .coerceAtLeast(0.2 * state.world.inflation)
    }

    fun repriceAll(state: GameState): GameState =
        state.copy(airlines = state.airlines.map { it.copy(sharePrice = price(state, it)) })

    /** 시장에 남은 주식 (누구도 들고 있지 않은 몫). */
    fun floatShares(state: GameState, targetId: String): Double {
        val target = state.airline(targetId)
        val held = state.airlines.sumOf { it.holdings[targetId] ?: 0.0 }
        return (target.shares - held).coerceAtLeast(0.0)
    }

    fun ownershipRatio(state: GameState, holderId: String, targetId: String): Double {
        val target = state.airlineOrNull(targetId) ?: return 0.0
        val held = state.airlineOrNull(holderId)?.holdings?.get(targetId) ?: 0.0
        return if (target.shares <= 0) 0.0 else held / target.shares
    }

    /** 대량 매집에는 프리미엄이 붙고, 지분이 쌓일수록 더 비싸진다. */
    fun buyCost(state: GameState, buyerId: String, targetId: String, shares: Double): Double {
        val target = state.airline(targetId)
        val held = state.airlineOrNull(buyerId)?.holdings?.get(targetId) ?: 0.0
        val stakeAfter = ((held + shares) / target.shares.coerceAtLeast(1.0)).coerceIn(0.0, 1.0)
        val premium = Balance.TENDER_PREMIUM + stakeAfter * Balance.TENDER_ESCALATION
        return shares * target.sharePrice * premium
    }

    /**
     * 한 분기에 더 사들일 수 있는 주식 수. 이번 분기에 이미 매수한 몫을 빼야
     * 여러 번 나눠 사서 상한을 우회하는 일이 막힌다.
     */
    fun maxBuyableThisQuarter(state: GameState, buyerId: String, targetId: String): Double {
        val target = state.airline(targetId)
        val already = state.airlineOrNull(buyerId)?.boughtThisQuarter?.get(targetId) ?: 0.0
        val cap = target.shares * Balance.MAX_STAKE_PER_QUARTER - already
        return minOf(cap, floatShares(state, targetId)).coerceAtLeast(0.0)
    }

    /**
     * 지분 50% 초과를 확보한 항공사가 있으면 흡수합병한다.
     * 플레이어가 인수당하면 게임이 끝난다.
     */
    fun settleTakeovers(state: GameState): GameState {
        var s = state
        var guard = 0
        while (guard++ < 8) {
            val pair = s.airlines
                .filter { it.alive }
                .firstNotNullOfOrNull { holder ->
                    holder.holdings.entries
                        .firstOrNull { (targetId, sh) ->
                            targetId != holder.id &&
                                s.airlineOrNull(targetId)?.alive == true &&
                                sh / s.airline(targetId).shares > Balance.TAKEOVER_THRESHOLD
                        }
                        ?.let { holder.id to it.key }
                }
            if (pair == null) break
            s = merge(s, pair.first, pair.second)
        }
        return s
    }

    /** 흡수합병 — 기재·노선·슬롯·부대사업·현금과 부채를 인수사가 모두 넘겨받는다. */
    fun merge(state: GameState, acquirerId: String, targetId: String): GameState {
        val acquirer = state.airline(acquirerId)
        val target = state.airline(targetId)

        var routes = state.routes.map {
            if (it.airlineId == targetId) it.copy(airlineId = acquirerId) else it
        }
        routes = mergeDuplicateRoutes(routes, acquirerId)
        // 중복 노선이 합쳐지면서 없어진 노선 id 가 있으므로 기재의 배속을 다시 맺어준다.
        val routeOfPlane = routes.flatMap { r -> r.planeIds.map { it to r.id } }.toMap()
        val planes = state.planes.map {
            val owner = if (it.airlineId == targetId) acquirerId else it.airlineId
            it.copy(airlineId = owner, routeId = routeOfPlane[it.id])
        }

        val mergedSlots = buildMap {
            putAll(acquirer.slots)
            for ((city, n) in target.slots) put(city, (get(city) ?: 0) + n)
        }
        val mergedHoldings = buildMap {
            putAll(acquirer.holdings)
            for ((id, n) in target.holdings) if (id != acquirerId) put(id, (get(id) ?: 0.0) + n)
            remove(targetId)
        }
        // 상대가 우리 주식을 들고 있었다면 자사주가 되어 돌아온다. 그냥 버리면 자산이 증발하므로
        // 발행 주식 수에서 소각한다 (남은 주주의 지분율이 그만큼 올라간다).
        val treasury = target.holdings[acquirerId] ?: 0.0
        val acquirerShares = (acquirer.shares - treasury).coerceAtLeast(acquirer.shares * 0.1)

        // 다른 주주들은 시장가로 정리한다.
        val airlines = state.airlines.map { a ->
            when (a.id) {
                acquirerId -> a.copy(
                    cash = a.cash + target.cash,
                    debt = a.debt + target.debt,
                    shares = acquirerShares,
                    slots = mergedSlots,
                    holdings = mergedHoldings,
                    businesses = a.businesses + target.businesses,
                    brand = a.brand.mapValues { (r, v) ->
                        minOf(Balance.BRAND_MAX, v + (target.brand[r] ?: 0.0) * 0.4)
                    },
                )
                targetId -> a.copy(
                    alive = false,
                    mergedInto = acquirerId,
                    cash = 0.0,
                    debt = 0.0,
                    slots = emptyMap(),
                    holdings = emptyMap(),
                    businesses = emptyList(),
                )
                else -> {
                    val held = a.holdings[targetId] ?: 0.0
                    if (held <= 0.0) a
                    else a.copy(
                        cash = a.cash + held * target.sharePrice,
                        holdings = a.holdings - targetId,
                    )
                }
            }
        }

        val news = NewsItem(
            turn = state.turn,
            kind = NewsKind.MARKET,
            headline = "${acquirer.name}, ${target.name} 인수 완료",
            body = "${target.name}의 노선망과 기재가 ${acquirer.name}으로 넘어갔습니다.",
        )
        // 선금까지 치른 발주는 인수사가 이어받는다. 안 넘기면 죽은 회사 앞으로
        // 기재가 인도돼 그대로 증발한다.
        val orders = state.orders.map {
            if (it.airlineId == targetId) it.copy(airlineId = acquirerId) else it
        }
        return state.copy(
            airlines = airlines,
            planes = planes,
            routes = routes,
            orders = orders,
            news = state.news + news,
        )
    }

    /** 합병으로 같은 도시쌍에 자기 노선이 둘 생기면 하나로 합친다. */
    private fun mergeDuplicateRoutes(routes: List<Route>, airlineId: String): List<Route> {
        val mine = routes.filter { it.airlineId == airlineId }
        val others = routes.filter { it.airlineId != airlineId }
        val byPair = mine.groupBy { Geo.pairKey(it.from, it.to) }
        val merged = byPair.values.map { group ->
            if (group.size == 1) {
                group[0]
            } else {
                val head = group.first()
                head.copy(
                    freq = group.sumOf { it.freq },
                    planeIds = group.flatMap { it.planeIds },
                    fareMul = group.sumOf { it.fareMul } / group.size,
                    serviceExtra = group.maxOf { it.serviceExtra },
                    active = group.any { it.active },
                )
            }
        }
        return others + merged
    }
}
