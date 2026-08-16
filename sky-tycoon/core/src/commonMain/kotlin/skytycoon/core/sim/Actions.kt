package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.model.Airline
import skytycoon.core.model.AircraftType
import skytycoon.core.model.Business
import skytycoon.core.model.BusinessType
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsItem
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Order
import skytycoon.core.model.Plane
import skytycoon.core.model.Region
import skytycoon.core.model.Route

data class ActionResult(val state: GameState, val ok: Boolean, val message: String = "") {
    companion object {
        fun fail(state: GameState, message: String) = ActionResult(state, false, message)
    }
}

/** 플레이어와 AI 가 똑같이 쓰는 명령. 규칙 검증이 한 군데 모여 있어야 양쪽이 어긋나지 않는다. */
sealed interface Command {
    val airlineId: String

    data class OpenRoute(
        override val airlineId: String,
        val from: String,
        val to: String,
        val planeIds: List<Int>,
        val freq: Int,
        val fareMul: Double = 1.0,
        val serviceExtra: Int = 0,
    ) : Command

    data class CloseRoute(override val airlineId: String, val routeId: Int) : Command

    data class TuneRoute(
        override val airlineId: String,
        val routeId: Int,
        val freq: Int? = null,
        val fareMul: Double? = null,
        val serviceExtra: Int? = null,
    ) : Command

    data class AssignPlanes(
        override val airlineId: String,
        val routeId: Int,
        val planeIds: List<Int>,
    ) : Command

    data class BuySlots(override val airlineId: String, val city: String, val count: Int) : Command

    data class BuyAircraft(
        override val airlineId: String,
        val typeId: String,
        val count: Int,
        val used: Boolean = false,
    ) : Command

    data class SellAircraft(override val airlineId: String, val planeId: Int) : Command

    /** 양수면 차입, 음수면 상환. */
    data class Loan(override val airlineId: String, val amount: Double) : Command

    data class UpgradeService(override val airlineId: String) : Command

    data class SetAdBudget(override val airlineId: String, val region: Region, val amount: Double) : Command

    data class BuildBusiness(
        override val airlineId: String,
        val type: BusinessType,
        val city: String,
    ) : Command

    /** 양수면 매수, 음수면 매도. */
    data class TradeShares(
        override val airlineId: String,
        val targetId: String,
        val shares: Double,
    ) : Command

    /** 유상증자 — 자금을 조달하면서 적대적 매집자의 지분을 희석한다. */
    data class IssueShares(override val airlineId: String, val shares: Double) : Command
}

internal fun GameState.withAirline(id: String, f: (Airline) -> Airline): GameState =
    copy(airlines = airlines.map { if (it.id == id) f(it) else it })

internal fun GameState.withRoute(id: Int, f: (Route) -> Route): GameState =
    copy(routes = routes.map { if (it.id == id) f(it) else it })

object Actions {

    fun execute(state: GameState, cmd: Command): ActionResult {
        val airline = state.airlineOrNull(cmd.airlineId)
            ?: return ActionResult.fail(state, "존재하지 않는 항공사입니다.")
        if (!airline.alive) return ActionResult.fail(state, "이미 사라진 항공사입니다.")
        if (state.outcome != null) return ActionResult.fail(state, "게임이 이미 끝났습니다.")

        return when (cmd) {
            is Command.OpenRoute -> openRoute(state, airline, cmd)
            is Command.CloseRoute -> closeRoute(state, airline, cmd)
            is Command.TuneRoute -> tuneRoute(state, airline, cmd)
            is Command.AssignPlanes -> assignPlanes(state, airline, cmd)
            is Command.BuySlots -> buySlots(state, airline, cmd)
            is Command.BuyAircraft -> buyAircraft(state, airline, cmd)
            is Command.SellAircraft -> sellAircraft(state, airline, cmd)
            is Command.Loan -> loan(state, airline, cmd)
            is Command.UpgradeService -> upgradeService(state, airline)
            is Command.SetAdBudget -> setAdBudget(state, airline, cmd)
            is Command.BuildBusiness -> buildBusiness(state, airline, cmd)
            is Command.TradeShares -> tradeShares(state, airline, cmd)
            is Command.IssueShares -> issueShares(state, airline, cmd)
        }
    }

    /** 한 분기 증자 한도 — 기존 주식의 30%. */
    fun maxIssuable(airline: Airline): Double = airline.shares * 0.30

    /** 여러 명령을 순서대로. 하나라도 실패하면 그 지점까지의 상태와 실패 사유를 돌려준다. */
    fun executeAll(state: GameState, cmds: List<Command>): ActionResult {
        var s = state
        for (c in cmds) {
            val r = execute(s, c)
            if (!r.ok) return ActionResult(s, false, r.message)
            s = r.state
        }
        return ActionResult(s, true)
    }

    // ------------------------------------------------------------------ 노선

    fun routeSetupCost(state: GameState, from: String, to: String): Double =
        (200_000.0 + Geo.distance(from, to) * 40.0) * state.world.inflation

    private fun openRoute(state: GameState, airline: Airline, cmd: Command.OpenRoute): ActionResult {
        if (cmd.from == cmd.to) return ActionResult.fail(state, "같은 도시로는 노선을 열 수 없습니다.")
        val from = Cities.find(cmd.from) ?: return ActionResult.fail(state, "알 수 없는 도시입니다.")
        val to = Cities.find(cmd.to) ?: return ActionResult.fail(state, "알 수 없는 도시입니다.")
        if (state.cityState[from.id]?.isClosed(state.turn) == true ||
            state.cityState[to.id]?.isClosed(state.turn) == true
        ) {
            return ActionResult.fail(state, "공항이 폐쇄 중입니다.")
        }
        if (state.routes.any {
                it.airlineId == airline.id && it.active &&
                    Geo.pairKey(it.from, it.to) == Geo.pairKey(from.id, to.id)
            }
        ) {
            return ActionResult.fail(state, "이미 같은 구간에 노선이 있습니다.")
        }
        if (cmd.freq < 1) return ActionResult.fail(state, "주간 편수는 1 이상이어야 합니다.")

        val dist = Geo.distance(from.id, to.id)
        val planes = cmd.planeIds.map { id ->
            state.planes.firstOrNull { it.id == id }
                ?: return ActionResult.fail(state, "보유하지 않은 기재입니다.")
        }
        if (planes.any { it.airlineId != airline.id }) {
            return ActionResult.fail(state, "다른 항공사의 기재입니다.")
        }
        if (planes.any { it.routeId != null }) {
            return ActionResult.fail(state, "이미 다른 노선에 배속된 기재가 있습니다.")
        }
        if (planes.isEmpty()) return ActionResult.fail(state, "투입할 기재를 선택하세요.")
        val tooShort = planes.firstOrNull { !Economics.canFly(AircraftCatalog[it.typeId], dist) }
        if (tooShort != null) {
            val t = AircraftCatalog[tooShort.typeId]
            return ActionResult.fail(state, "${t.name}의 항속거리(${t.range}km)로는 ${dist.toInt()}km를 날 수 없습니다.")
        }

        val cap = Economics.capacity(planes, dist)
        if (cmd.freq > cap.maxFreq) {
            return ActionResult.fail(state, "기재 ${planes.size}대로는 주 ${cap.maxFreq}왕복이 한계입니다.")
        }
        if (state.freeSlots(airline.id, from.id) < cmd.freq) {
            return ActionResult.fail(state, "${from.name} 슬롯이 부족합니다 (여유 ${state.freeSlots(airline.id, from.id)}).")
        }
        if (state.freeSlots(airline.id, to.id) < cmd.freq) {
            return ActionResult.fail(state, "${to.name} 슬롯이 부족합니다 (여유 ${state.freeSlots(airline.id, to.id)}).")
        }

        val cost = routeSetupCost(state, from.id, to.id)
        if (airline.cash < cost) return ActionResult.fail(state, "개설 비용이 부족합니다.")

        val routeId = state.nextId
        val route = Route(
            id = routeId,
            airlineId = airline.id,
            from = from.id,
            to = to.id,
            fareMul = cmd.fareMul.coerceIn(Balance.FARE_MIN_MUL, Balance.FARE_MAX_MUL),
            freq = cmd.freq,
            planeIds = cmd.planeIds,
            serviceExtra = cmd.serviceExtra.coerceIn(0, 2),
        )
        val next = state
            .copy(
                routes = state.routes + route,
                nextId = routeId + 1,
                planes = state.planes.map { if (it.id in cmd.planeIds) it.copy(routeId = routeId) else it },
            )
            .withAirline(airline.id) { it.copy(cash = it.cash - cost) }
        return ActionResult(next, true, "${from.name}–${to.name} 노선을 개설했습니다.")
    }

    private fun closeRoute(state: GameState, airline: Airline, cmd: Command.CloseRoute): ActionResult {
        val route = state.routes.firstOrNull { it.id == cmd.routeId && it.airlineId == airline.id }
            ?: return ActionResult.fail(state, "노선을 찾을 수 없습니다.")
        val next = state.copy(
            routes = state.routes.filter { it.id != route.id },
            planes = state.planes.map { if (it.routeId == route.id) it.copy(routeId = null) else it },
        )
        return ActionResult(next, true, "${Cities.name(route.from)}–${Cities.name(route.to)} 노선을 폐지했습니다.")
    }

    private fun tuneRoute(state: GameState, airline: Airline, cmd: Command.TuneRoute): ActionResult {
        val route = state.routes.firstOrNull { it.id == cmd.routeId && it.airlineId == airline.id }
            ?: return ActionResult.fail(state, "노선을 찾을 수 없습니다.")
        val dist = Geo.distance(route.from, route.to)
        val planes = route.planeIds.mapNotNull { id -> state.planes.firstOrNull { it.id == id } }
        val cap = Economics.capacity(planes, dist)

        val newFreq = cmd.freq ?: route.freq
        if (newFreq < 0) return ActionResult.fail(state, "편수가 올바르지 않습니다.")
        if (newFreq > cap.maxFreq) {
            return ActionResult.fail(state, "배속된 기재로는 주 ${cap.maxFreq}왕복이 한계입니다.")
        }
        val delta = newFreq - route.freq
        if (delta > 0) {
            if (state.freeSlots(airline.id, route.from) < delta) {
                return ActionResult.fail(state, "${Cities.name(route.from)} 슬롯이 부족합니다.")
            }
            if (state.freeSlots(airline.id, route.to) < delta) {
                return ActionResult.fail(state, "${Cities.name(route.to)} 슬롯이 부족합니다.")
            }
        }

        val next = state.withRoute(route.id) {
            it.copy(
                freq = newFreq,
                fareMul = (cmd.fareMul ?: it.fareMul).coerceIn(Balance.FARE_MIN_MUL, Balance.FARE_MAX_MUL),
                serviceExtra = (cmd.serviceExtra ?: it.serviceExtra).coerceIn(0, 2),
                active = newFreq > 0,
            )
        }
        return ActionResult(next, true)
    }

    private fun assignPlanes(state: GameState, airline: Airline, cmd: Command.AssignPlanes): ActionResult {
        val route = state.routes.firstOrNull { it.id == cmd.routeId && it.airlineId == airline.id }
            ?: return ActionResult.fail(state, "노선을 찾을 수 없습니다.")
        val dist = Geo.distance(route.from, route.to)
        val planes = cmd.planeIds.map { id ->
            state.planes.firstOrNull { it.id == id }
                ?: return ActionResult.fail(state, "보유하지 않은 기재입니다.")
        }
        if (planes.any { it.airlineId != airline.id }) return ActionResult.fail(state, "다른 항공사의 기재입니다.")
        if (planes.any { it.routeId != null && it.routeId != route.id }) {
            return ActionResult.fail(state, "다른 노선에 배속된 기재가 있습니다.")
        }
        val tooShort = planes.firstOrNull { !Economics.canFly(AircraftCatalog[it.typeId], dist) }
        if (tooShort != null) {
            return ActionResult.fail(state, "${AircraftCatalog[tooShort.typeId].name}의 항속거리가 모자랍니다.")
        }

        val cap = Economics.capacity(planes, dist)
        val freq = route.freq.coerceAtMost(cap.maxFreq)
        val next = state
            .copy(
                planes = state.planes.map {
                    when {
                        it.id in cmd.planeIds -> it.copy(routeId = route.id)
                        it.routeId == route.id -> it.copy(routeId = null)
                        else -> it
                    }
                },
            )
            .withRoute(route.id) { it.copy(planeIds = cmd.planeIds, freq = freq, active = freq > 0) }
        return ActionResult(next, true)
    }

    // ------------------------------------------------------------------ 슬롯

    /** 슬롯을 한 개씩 사면서 희소성에 따라 오르는 가격을 누적한다. */
    fun slotCost(state: GameState, airlineId: String, city: String, count: Int): Double {
        var s = state
        var total = 0.0
        repeat(count) {
            total += Economics.slotPrice(s, airlineId, city)
            s = s.withAirline(airlineId) { a -> a.copy(slots = a.slots + (city to a.slotsAt(city) + 1)) }
        }
        return total
    }

    private fun buySlots(state: GameState, airline: Airline, cmd: Command.BuySlots): ActionResult {
        val city = Cities.find(cmd.city) ?: return ActionResult.fail(state, "알 수 없는 도시입니다.")
        if (cmd.count < 1) return ActionResult.fail(state, "1개 이상 매입해야 합니다.")
        val free = state.unsoldSlots(city)
        if (free < cmd.count) return ActionResult.fail(state, "${city.name}에 남은 슬롯이 ${free}개뿐입니다.")

        val cost = slotCost(state, airline.id, city.id, cmd.count)
        if (airline.cash < cost) return ActionResult.fail(state, "슬롯 매입 자금이 부족합니다.")

        val next = state.withAirline(airline.id) {
            it.copy(
                cash = it.cash - cost,
                slots = it.slots + (city.id to it.slotsAt(city.id) + cmd.count),
            )
        }
        return ActionResult(next, true, "${city.name} 슬롯 ${cmd.count}개를 확보했습니다.")
    }

    // ------------------------------------------------------------------ 기재

    fun usedPrice(type: AircraftType, ageQuarters: Int): Double =
        type.price * AircraftCatalog.residualRatio(ageQuarters) * Balance.USED_PRICE_MUL

    fun sellPrice(type: AircraftType, ageQuarters: Int): Double =
        type.price * AircraftCatalog.residualRatio(ageQuarters) * Balance.SELL_PENALTY

    private fun buyAircraft(state: GameState, airline: Airline, cmd: Command.BuyAircraft): ActionResult {
        if (cmd.count < 1) return ActionResult.fail(state, "1대 이상 발주해야 합니다.")
        val type = AircraftCatalog.find(cmd.typeId) ?: return ActionResult.fail(state, "알 수 없는 기종입니다.")

        if (cmd.used) {
            if (state.year <= type.year + 2) return ActionResult.fail(state, "${type.name}은 아직 중고 매물이 없습니다.")
            if (state.year > type.retire + 12) return ActionResult.fail(state, "${type.name}은 시장에서 자취를 감췄습니다.")
            val rng = Rng(state.rngState)
            var cash = airline.cash
            val newPlanes = mutableListOf<Plane>()
            var nextId = state.nextId
            repeat(cmd.count) {
                val age = rng.int(20, 52)
                val price = usedPrice(type, age)
                if (cash < price) return@repeat
                cash -= price
                newPlanes += Plane(id = nextId++, typeId = type.id, airlineId = airline.id, ageQuarters = age)
            }
            if (newPlanes.isEmpty()) return ActionResult.fail(state, "중고기 구입 자금이 부족합니다.")
            val next = state
                .copy(planes = state.planes + newPlanes, nextId = nextId, rngState = rng.state)
                .withAirline(airline.id) { it.copy(cash = cash) }
            return ActionResult(next, true, "${type.name} 중고 ${newPlanes.size}대를 인수했습니다.")
        }

        if (state.year < type.year) return ActionResult.fail(state, "${type.name}은 ${type.year}년부터 인도됩니다.")
        if (state.year > type.retire) return ActionResult.fail(state, "${type.name}은 생산이 끝났습니다. 중고로 알아보세요.")
        val cost = type.price * cmd.count
        if (airline.cash < cost) return ActionResult.fail(state, "발주 대금이 부족합니다.")

        val order = Order(
            id = state.nextId,
            airlineId = airline.id,
            typeId = type.id,
            count = cmd.count,
            deliverTurn = state.turn + Balance.ORDER_DELAY_QUARTERS,
        )
        val next = state
            .copy(orders = state.orders + order, nextId = state.nextId + 1)
            .withAirline(airline.id) { it.copy(cash = it.cash - cost) }
        return ActionResult(
            next,
            true,
            "${type.name} ${cmd.count}대를 발주했습니다 (${Balance.ORDER_DELAY_QUARTERS}분기 뒤 인도).",
        )
    }

    private fun sellAircraft(state: GameState, airline: Airline, cmd: Command.SellAircraft): ActionResult {
        val plane = state.planes.firstOrNull { it.id == cmd.planeId && it.airlineId == airline.id }
            ?: return ActionResult.fail(state, "보유하지 않은 기재입니다.")
        if (plane.routeId != null) return ActionResult.fail(state, "노선에 배속된 기재는 먼저 배속을 풀어야 합니다.")
        val type = AircraftCatalog[plane.typeId]
        val proceeds = sellPrice(type, plane.ageQuarters)
        val next = state
            .copy(planes = state.planes.filter { it.id != plane.id })
            .withAirline(airline.id) { it.copy(cash = it.cash + proceeds) }
        return ActionResult(next, true, "${type.name} 1대를 매각했습니다.")
    }

    // ------------------------------------------------------------------ 재무

    fun debtCapacity(state: GameState, airline: Airline): Double {
        val bonus = if (airline.bonusKey == "finance") 1.4 else 1.0
        return (Economics.equity(state, airline) * Balance.DEBT_CAP_EQUITY * bonus).coerceAtLeast(0.0)
    }

    fun interestRate(state: GameState, airline: Airline): Double =
        (state.world.interest - if (airline.bonusKey == "finance") 0.01 else 0.0).coerceAtLeast(0.01)

    private fun loan(state: GameState, airline: Airline, cmd: Command.Loan): ActionResult {
        if (cmd.amount > 0) {
            val cap = debtCapacity(state, airline)
            if (airline.debt + cmd.amount > cap) {
                return ActionResult.fail(state, "차입 한도를 넘습니다 (한도 ${(cap / 1e6).toInt()}백만 달러).")
            }
            val next = state.withAirline(airline.id) {
                it.copy(cash = it.cash + cmd.amount, debt = it.debt + cmd.amount)
            }
            return ActionResult(next, true, "${(cmd.amount / 1e6).toInt()}백만 달러를 차입했습니다.")
        }
        val repay = -cmd.amount
        if (repay <= 0) return ActionResult.fail(state, "금액이 올바르지 않습니다.")
        if (airline.cash < repay) return ActionResult.fail(state, "상환할 현금이 부족합니다.")
        if (airline.debt < repay) return ActionResult.fail(state, "부채보다 많이 상환할 수 없습니다.")
        val next = state.withAirline(airline.id) {
            it.copy(cash = it.cash - repay, debt = it.debt - repay)
        }
        return ActionResult(next, true, "${(repay / 1e6).toInt()}백만 달러를 상환했습니다.")
    }

    fun serviceUpgradeCost(state: GameState, airline: Airline): Double =
        Balance.SERVICE_UPGRADE_COST * state.world.inflation * airline.serviceLevel

    private fun upgradeService(state: GameState, airline: Airline): ActionResult {
        if (airline.serviceLevel >= 5) return ActionResult.fail(state, "이미 최고 등급입니다.")
        val cost = serviceUpgradeCost(state, airline)
        if (airline.cash < cost) return ActionResult.fail(state, "서비스 개선 자금이 부족합니다.")
        val next = state.withAirline(airline.id) {
            it.copy(cash = it.cash - cost, serviceLevel = it.serviceLevel + 1)
        }
        return ActionResult(next, true, "기내 서비스가 ${airline.serviceLevel + 1}등급으로 올라갔습니다.")
    }

    private fun setAdBudget(state: GameState, airline: Airline, cmd: Command.SetAdBudget): ActionResult {
        if (cmd.amount < 0) return ActionResult.fail(state, "광고비가 올바르지 않습니다.")
        val next = state.withAirline(airline.id) {
            it.copy(adBudget = it.adBudget + (cmd.region to cmd.amount))
        }
        return ActionResult(next, true)
    }

    private fun buildBusiness(state: GameState, airline: Airline, cmd: Command.BuildBusiness): ActionResult {
        val city = Cities.find(cmd.city) ?: return ActionResult.fail(state, "알 수 없는 도시입니다.")
        if (airline.slotsAt(city.id) <= 0) {
            return ActionResult.fail(state, "${city.name}에 취항 슬롯이 있어야 사업을 낼 수 있습니다.")
        }
        if (airline.businesses.any { it.type == cmd.type && it.city == city.id }) {
            return ActionResult.fail(state, "${city.name}에 이미 ${cmd.type.label}이(가) 있습니다.")
        }
        val cost = cmd.type.cost * state.world.inflation
        if (airline.cash < cost) return ActionResult.fail(state, "사업 자금이 부족합니다.")
        val next = state.withAirline(airline.id) {
            it.copy(cash = it.cash - cost, businesses = it.businesses + Business(cmd.type, city.id))
        }
        return ActionResult(next, true, "${city.name}에 ${cmd.type.label}을(를) 열었습니다.")
    }

    // ------------------------------------------------------------------ 주식

    private fun tradeShares(state: GameState, airline: Airline, cmd: Command.TradeShares): ActionResult {
        if (cmd.targetId == airline.id) return ActionResult.fail(state, "자사주는 거래할 수 없습니다.")
        val target = state.airlineOrNull(cmd.targetId)
            ?: return ActionResult.fail(state, "대상 항공사를 찾을 수 없습니다.")
        if (!target.alive) return ActionResult.fail(state, "이미 사라진 항공사입니다.")

        if (cmd.shares > 0) {
            val limit = Stock.maxBuyableThisQuarter(state, airline.id, target.id)
            if (limit <= 0.0) return ActionResult.fail(state, "이번 분기에 더 사들일 물량이 없습니다.")
            if (cmd.shares > limit) {
                val pct = (Balance.MAX_STAKE_PER_QUARTER * 100).toInt()
                return ActionResult.fail(
                    state,
                    "한 분기에 살 수 있는 물량을 넘습니다 (지분 ${pct}% · ${(limit / 1e6).toInt()}백만 주까지).",
                )
            }
            val cost = Stock.buyCost(state, airline.id, target.id, cmd.shares)
            if (airline.cash < cost) return ActionResult.fail(state, "매수 자금이 부족합니다.")
            var next = state.withAirline(airline.id) {
                it.copy(
                    cash = it.cash - cost,
                    holdings = it.holdings + (target.id to (it.holdings[target.id] ?: 0.0) + cmd.shares),
                )
            }
            // 우리 회사가 노려지고 있다면 반드시 알려준다 — 모르고 당하면 게임이 아니다.
            if (target.isPlayer) {
                val stake = Stock.ownershipRatio(next, airline.id, target.id)
                next = next.copy(
                    news = next.news + NewsItem(
                        turn = next.turn,
                        kind = NewsKind.MARKET,
                        headline = "${airline.name}, 귀사 지분 ${(stake * 100).toInt()}% 확보",
                        body = if (stake >= 0.33) {
                            "경영권이 위태롭습니다. 유상증자로 지분을 희석하거나 실적으로 주가를 끌어올리세요."
                        } else {
                            "적대적 인수 시도로 보입니다. 지분 50%를 넘기면 회사를 잃습니다."
                        },
                    ),
                )
            }
            return ActionResult(Stock.settleTakeovers(next), true, "${target.name} 주식을 매수했습니다.")
        }

        val sell = -cmd.shares
        val held = airline.holdings[target.id] ?: 0.0
        if (sell <= 0) return ActionResult.fail(state, "수량이 올바르지 않습니다.")
        if (held < sell) return ActionResult.fail(state, "보유 주식이 부족합니다.")
        val proceeds = sell * target.sharePrice * 0.95
        val next = state.withAirline(airline.id) {
            val left = held - sell
            it.copy(
                cash = it.cash + proceeds,
                holdings = if (left <= 1e-9) it.holdings - target.id else it.holdings + (target.id to left),
            )
        }
        return ActionResult(next, true, "${target.name} 주식을 매도했습니다.")
    }

    private fun issueShares(state: GameState, airline: Airline, cmd: Command.IssueShares): ActionResult {
        if (cmd.shares <= 0) return ActionResult.fail(state, "발행 주식 수가 올바르지 않습니다.")
        val limit = maxIssuable(airline)
        if (cmd.shares > limit) {
            return ActionResult.fail(state, "한 분기 증자 한도는 ${(limit / 1e6).toInt()}백만 주입니다.")
        }
        val proceeds = cmd.shares * airline.sharePrice * Balance.ISSUE_DISCOUNT
        val next = state.withAirline(airline.id) {
            it.copy(cash = it.cash + proceeds, shares = it.shares + cmd.shares)
        }
        return ActionResult(
            next,
            true,
            "${(cmd.shares / 1e6).toInt()}백만 주를 발행해 ${(proceeds / 1e6).toInt()}백만 달러를 조달했습니다.",
        )
    }
}
