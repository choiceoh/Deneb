package skytycoon.core.sim

import skytycoon.core.model.Airline
import skytycoon.core.model.Business
import skytycoon.core.model.BusinessType
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

    /** 인수가 성립하면 인수사가 소수 주주에게 치러야 하는 잔여 지분 정리 대금. */
    fun minorityStakeValue(state: GameState, acquirerId: String, targetId: String): Double {
        val target = state.airlineOrNull(targetId) ?: return 0.0
        return state.airlines
            .filter { it.id != acquirerId && it.id != targetId }
            .sumOf { (it.holdings[targetId] ?: 0.0) * target.sharePrice }
    }

    /** 매수를 상태에 반영한다. 사전 검증과 실제 실행이 **같은 코드**를 써야 어긋나지 않는다. */
    fun applyPurchase(
        state: GameState,
        buyerId: String,
        targetId: String,
        shares: Double,
        cost: Double,
    ): GameState = state.withAirline(buyerId) {
        it.copy(
            cash = it.cash - cost,
            holdings = it.holdings + (targetId to (it.holdings[targetId] ?: 0.0) + shares),
            boughtThisQuarter = it.boughtThisQuarter +
                (targetId to (it.boughtThisQuarter[targetId] ?: 0.0) + shares),
        )
    }

    /**
     * 이 매수를 감당할 수 있는가 — **연쇄 인수까지 끝난 모습**으로 판단한다.
     *
     * 직접 대상만 재면 두 군데서 틀린다. (1) 과반을 넘기는 순간 잔여 지분 정리 대금이
     * 따라붙는데 매수 대금만 보고 통과시키면 견적에 없던 청구가 떨어진다. (2) 상대가
     * 들고 있던 지분을 물려받아 그 자리에서 다음 회사까지 삼키면 빚이 한 번 더 불어난다.
     * 실제로 굴려 보는 게 두 경우를 다 덮는 유일한 방법이다.
     *
     * UI 버튼과 명령이 이 함수 하나를 공유해야 "제안한 물량이 실행에서 튕기는" 일이 없다.
     */
    fun canAfford(state: GameState, buyerId: String, targetId: String, shares: Double): Boolean {
        if (shares <= 0.0) return false
        val buyer = state.airlineOrNull(buyerId) ?: return false
        val cost = buyCost(state, buyerId, targetId, shares)
        if (buyer.cash < cost) return false

        val settled = settleTakeovers(applyPurchase(state, buyerId, targetId, shares, cost))
        val after = settled.airlineOrNull(buyerId) ?: return false
        if (!after.alive) return false
        // 인수로 자산이 늘면 차입 여력도 함께 늘어난다 — 그 늘어난 여력을 기준으로 잰다.
        return after.debt <= Actions.debtCapacity(settled, after)
    }

    /**
     * 지금 실행 가능한 최대 매수량. 매집 단가가 지분율에 따라 오르고 과반을 넘는 순간
     * 정리 대금이 붙어 역산이 어렵다 — [canAfford] 를 술어로 이분 탐색한다.
     *
     * 이게 없으면 UI 가 "이번 분기 한도 전액"만 살 수 있게 만들어, 45% 를 쥐고
     * 6% 는 살 수 있는 플레이어가 인수를 끝내지 못한다.
     */
    fun affordableShares(state: GameState, buyerId: String, targetId: String): Double {
        var lo = 0.0
        var hi = maxBuyableThisQuarter(state, buyerId, targetId)
        if (hi <= 0.0) return 0.0
        if (canAfford(state, buyerId, targetId, hi)) return hi
        // 비용은 매수량에 대해 단조 증가라 (정리 대금은 과반에서 한 번 계단으로 뛴다)
        // 이분 탐색이 성립한다.
        repeat(24) {
            val mid = (lo + hi) / 2
            if (canAfford(state, buyerId, targetId, mid)) lo = mid else hi = mid
        }
        return lo
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

        // 소수 주주를 시장가로 몰아내는 값은 인수사가 낸다. 어디서도 빼지 않으면
        // 과반만 사고도 회사 전체를 가져가면서 시장 전체 현금이 불어난다.
        val minorityPayout = minorityStakeValue(state, acquirerId, targetId)

        // 같은 도시에 같은 시설이 둘 생기지 않게 정리한다. BuildBusiness 가 막는 조합이라
        // 합병으로만 만들어질 수 있고, 그대로 두면 수입·브랜드·자산이 이중으로 잡힌다.
        // 겹치는 쪽은 **팔아서** 장부가를 현금으로 돌려준다 — 그냥 지우면 인수사가
        // 주식값에 얹어 이미 값을 치른 자산이 소리 없이 증발한다.
        val mergedBusinesses = mutableListOf<Business>()
        val soldOff = mutableListOf<Business>()
        for (b in acquirer.businesses + target.businesses) {
            val sameSite = mergedBusinesses.any { it.type == b.type && it.city == b.city }
            // 정비창은 회사에 하나뿐이라는 규칙도 지킨다 (정비 할인은 어차피 한 번만 먹는다).
            val extraHangar = b.type == BusinessType.HANGAR &&
                mergedBusinesses.any { it.type == BusinessType.HANGAR }
            if (sameSite || extraHangar) soldOff += b else mergedBusinesses += b
        }
        val disposalProceeds = Economics.businessValue(state, soldOff)

        // 인수 대금을 감당 못 하면 그 차액만큼 차입한다. 현금을 마이너스로 두면
        // 다음 분기 결산 전까지 화면에 음수 잔고가 그대로 뜬다.
        val pooled = acquirer.cash + target.cash + disposalProceeds
        val financed = (minorityPayout - pooled).coerceAtLeast(0.0)

        // 다른 주주들은 시장가로 정리한다.
        val airlines = state.airlines.map { a ->
            when (a.id) {
                acquirerId -> a.copy(
                    cash = (pooled - minorityPayout).coerceAtLeast(0.0),
                    debt = a.debt + target.debt + financed,
                    shares = acquirerShares,
                    slots = mergedSlots,
                    holdings = mergedHoldings,
                    businesses = mergedBusinesses,
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

        // 잔여 지분 정리에 얼마가 나갔는지는 알려줘야 한다 — 인수 직후 현금이 비면
        // 플레이어는 이유를 모른 채 자금난에 빠진다.
        val settlementNote = when {
            financed > 0 -> " 잔여 지분 정리에 ${millions(minorityPayout)}이 들어 ${millions(financed)}을 차입했습니다."
            minorityPayout > 0 -> " 잔여 지분 정리에 ${millions(minorityPayout)}을 지급했습니다."
            else -> ""
        }
        val news = NewsItem(
            turn = state.turn,
            kind = NewsKind.MARKET,
            headline = "${acquirer.name}, ${target.name} 인수 완료",
            body = "${target.name}의 노선망과 기재가 ${acquirer.name}으로 넘어갔습니다.$settlementNote",
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

    /** 뉴스 본문용 간단 금액 표기 (UI 포맷터는 core 에 없다). */
    private fun millions(v: Double): String = "${(v / 1e6).toInt()}백만 달러"
}
