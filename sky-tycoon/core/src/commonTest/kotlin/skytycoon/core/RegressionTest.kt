package skytycoon.core

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.model.BusinessType
import skytycoon.core.model.CityState
import skytycoon.core.model.GameState
import skytycoon.core.model.Order
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Balance
import skytycoon.core.sim.Command
import skytycoon.core.sim.Economics
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Stock
import skytycoon.core.sim.TurnEngine
import kotlin.math.abs
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * 코드 리뷰에서 드러난 결함들의 재발 방지선.
 * 대부분 "UI·문서가 약속한 규칙을 코드가 안 지키던" 종류라, 규칙 쪽을 기준으로 못을 박는다.
 */
class RegressionTest {

    private fun fresh(): GameState = NewGame.create(seed = 4711, companyId = "hanseong")

    // ------------------------------------------------------------ 지분·증자 상한

    @Test
    fun `지분 매입 상한은 분기 누적으로 걸린다`() {
        var s = fresh()
        s = Actions.execute(s, Command.Loan("hanseong", Actions.debtCapacity(s, s.player) * 0.95)).state
        val target = s.airline("fuji")
        val chunk = target.shares * Balance.MAX_STAKE_PER_QUARTER

        val first = Actions.execute(s, Command.TradeShares("hanseong", "fuji", chunk))
        assertTrue(first.ok, first.message)
        s = first.state

        // 같은 분기에 한 번 더 사면 막혀야 한다 — 이게 뚫리면 한 턴에 인수가 끝난다.
        assertEquals(0.0, Stock.maxBuyableThisQuarter(s, "hanseong", "fuji"))
        val second = Actions.execute(s, Command.TradeShares("hanseong", "fuji", chunk))
        assertFalse(second.ok, "같은 분기에 상한을 넘겨 또 사들일 수 있으면 안 된다")

        // 분기가 넘어가면 다시 열린다.
        s = TurnEngine.advance(s)
        assertTrue(Stock.maxBuyableThisQuarter(s, "hanseong", "fuji") > 0.0)
    }

    @Test
    fun `유상증자도 분기 누적으로 걸린다`() {
        var s = fresh()
        val before = s.player.shares
        val first = Actions.execute(s, Command.IssueShares("hanseong", Actions.maxIssuable(s.player)))
        assertTrue(first.ok, first.message)
        s = first.state

        assertEquals(0.0, Actions.maxIssuable(s.player))
        assertFalse(
            Actions.execute(s, Command.IssueShares("hanseong", 1e6)).ok,
            "증자 버튼을 연타해 현금을 무한히 찍어낼 수 있으면 안 된다",
        )
        assertTrue(s.player.shares <= before * 1.3 + 1.0, "한 분기 발행량이 30%를 넘었다")
    }

    // ------------------------------------------------------------ 발주 인도

    @Test
    fun `발주한 기재는 정확히 두 분기 뒤에 손에 들어온다`() {
        var s = fresh()
        val before = s.planesOf("hanseong").size
        s = Actions.execute(s, Command.BuyAircraft("hanseong", "b727", 2)).state

        s = TurnEngine.advance(s)
        assertEquals(before, s.planesOf("hanseong").size, "한 분기 만에 도착하면 안 된다")
        s = TurnEngine.advance(s)
        assertEquals(before + 2, s.planesOf("hanseong").size, "두 분기 뒤에는 도착해 있어야 한다")
        assertTrue(
            s.planesOf("hanseong").any { it.ageQuarters == 0 },
            "갓 인도된 기재는 기령 0이어야 한다",
        )
    }

    @Test
    fun `인수하면 상대의 발주 잔량까지 넘어온다`() {
        var s = fresh()
        s = s.copy(
            orders = s.orders + Order(90001, "fuji", "b727", 3, s.turn + 2),
        )
        s = Stock.merge(s, "hanseong", "fuji")
        assertTrue(
            s.orders.all { it.airlineId != "fuji" },
            "사라진 회사 앞으로 발주가 남아 있으면 기재가 증발한다",
        )
        assertEquals(3, s.orders.first { it.id == 90001 }.count)
        assertEquals("hanseong", s.orders.first { it.id == 90001 }.airlineId)
    }

    // ------------------------------------------------------------ 공항 폐쇄

    @Test
    fun `공항이 닫힌 분기에는 운항 원가가 청구되지 않는다`() {
        var s = fresh()
        val route = s.routesOf("hanseong").first()
        s = s.copy(
            cityState = s.cityState + (route.to to (s.cityState[route.to] ?: CityState())
                .copy(closedUntilTurn = s.turn + 1)),
        )
        s = TurnEngine.advance(s)
        val settled = s.routes.first { it.id == route.id }
        assertEquals(0.0, settled.last?.cost ?: 0.0, "뜨지도 않은 비행기의 연료·승무원비를 받으면 안 된다")
        assertEquals(0.0, settled.last?.pax ?: 0.0)
    }

    // ------------------------------------------------------------ 자산 가치

    @Test
    fun `내용연수를 넘긴 기재는 더 이상 상각하지 않는다`() {
        val young = listOf(skytycoon.core.model.Plane(1, "b727", "x", ageQuarters = 4))
        val old = listOf(skytycoon.core.model.Plane(2, "b727", "x", ageQuarters = 200))
        assertTrue(Economics.depreciation(young) > 0.0)
        assertEquals(0.0, Economics.depreciation(old), "다 상각된 기재가 계속 비용을 만들면 안 된다")
    }

    @Test
    fun `발주 선급금은 인도 전에도 자산으로 잡힌다`() {
        var s = fresh()
        val before = Economics.equity(s, s.player)
        val r = Actions.execute(s, Command.BuyAircraft("hanseong", "b747_100", 2))
        assertTrue(r.ok, r.message)
        s = r.state
        val after = Economics.equity(s, s.player)
        assertTrue(
            after > before * 0.98,
            "발주만 했는데 기업가치가 ${((before - after) / 1e6).toInt()}M 증발했다 (선급금 미인식)",
        )
    }

    @Test
    fun `잔존가치는 연 단위로 감가한다`() {
        // 분기마다 깎으면 15년 만에 하한까지 떨어져 기업가치가 통째로 무너진다.
        val at15 = AircraftCatalog.residualRatio(15 * 4)
        val at25 = AircraftCatalog.residualRatio(25 * 4)
        assertTrue(at15 in 0.28..0.42, "15년 기령 잔존가치가 $at15 로 상식 밖이다")
        assertTrue(at25 < at15)
        assertTrue(at25 >= 0.12)
    }

    @Test
    fun `부대사업은 산 값에 걸맞은 자산가치를 갖는다`() {
        // 후기 시나리오는 물가가 5배가 넘는다. 원가만 명목으로 잡으면 지을 때마다 기업가치가 깎인다.
        var s = NewGame.create(scenarioId = "modern", companyId = "hanseong", seed = 3)
        val city = s.routesOf("hanseong").first().from
        val before = Economics.equity(s, s.player)
        val cost = BusinessType.HOTEL.cost * s.world.inflation
        val r = Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HOTEL, city))
        assertTrue(r.ok, r.message)
        s = r.state
        val after = Economics.equity(s, s.player)
        assertTrue(
            after > before - cost * 0.45,
            "호텔 하나 지었다고 기업가치가 ${((before - after) / 1e6).toInt()}M 증발하면 안 된다",
        )
    }

    // ------------------------------------------------------------ 세계 진행

    @Test
    fun `첫 분기가 한 해 앞선 물가로 굴러가지 않는다`() {
        val s = fresh()
        val started = s.world.inflation
        val after = TurnEngine.advance(s)
        assertEquals(
            started,
            after.world.inflation,
            "시작 연도 1분기를 넘기는 것만으로 물가가 한 해 뛰면 안 된다",
        )
        // 4분기를 넘겨 새해에 들어서는 순간 그 해 물가가 적용돼 있어야 한다
        // (1분기 계획을 작년 가격으로 세우면 안 된다).
        var t = s
        repeat(4) { t = TurnEngine.advance(t) }
        assertTrue(t.year > s.year, "4분기를 넘겼는데 해가 바뀌지 않았다")
        assertTrue(t.world.inflation > started, "새해에 들어섰는데 물가가 작년 그대로다")
    }

    // ------------------------------------------------------------ 광고·부대사업

    @Test
    fun `브랜드는 실제로 청구된 광고비만큼만 오른다`() {
        var s = fresh()
        val region = skytycoon.core.model.Region.AS
        val brandBefore = s.player.brandIn(region)
        s = Actions.execute(s, Command.SetAdBudget("hanseong", region, 8e6)).state
        s = TurnEngine.advance(s)

        val charged = s.player.lastResult!!.adSpend
        val expected = brandBefore * Balance.BRAND_DECAY +
            charged / 1e6 * Balance.AD_EFFICIENCY / s.world.inflation
        val actual = s.player.brandIn(region)
        assertTrue(
            abs(actual - expected.coerceAtMost(Balance.BRAND_MAX)) < 0.5,
            "청구액($charged)과 브랜드 반영이 어긋난다: 기대 $expected, 실제 $actual",
        )
    }

    @Test
    fun `부대사업은 취항 중인 도시에만 낼 수 있다`() {
        var s = fresh()
        // 슬롯만 사둔 도시에는 낼 수 없어야 한다.
        s = Actions.execute(s, Command.BuySlots("hanseong", "bangkok", 3)).state
        assertFalse(
            Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HOTEL, "bangkok")).ok,
            "슬롯만 사두고 사업을 낼 수 있으면 규칙 설명과 어긋난다",
        )
        val served = s.routesOf("hanseong").first().to
        assertTrue(Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HOTEL, served)).ok)
    }

    @Test
    fun `정비창은 전사에 하나만 둘 수 있다`() {
        var s = fresh()
        val cities = s.routesOf("hanseong").map { it.to }.distinct()
        s = Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HANGAR, cities[0])).state
        assertFalse(
            Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HANGAR, cities[1])).ok,
            "두 번째 정비창은 돈만 나가고 효과가 없으므로 막아야 한다",
        )
    }

    // ------------------------------------------------------------ 중고 시장

    @Test
    fun `중고 시세는 화면과 매입가가 같고 기령이 취항 연도 이후다`() {
        val s = NewGame.create(scenarioId = "modern", companyId = "liberty", seed = 8)
        for (type in AircraftCatalog.usedFor(s.year)) {
            val age = Actions.usedAge(s, type.id)
            val builtYear = s.year - age / 4
            assertTrue(
                builtYear >= type.year,
                "${type.name}(${type.year}년 취항)이 ${builtYear}년산으로 나왔다",
            )
            // 같은 분기 안에서는 몇 번을 조회해도 같은 값이어야 화면과 결제가 어긋나지 않는다.
            assertEquals(age, Actions.usedAge(s, type.id))
            assertEquals(Actions.usedPrice(type, age), Actions.usedPrice(s, type.id))
        }
    }

    @Test
    fun `중고기는 부분 체결 없이 견적대로 전부 사거나 못 산다`() {
        var s = NewGame.create(scenarioId = "modern", companyId = "liberty", seed = 8)
        val type = AircraftCatalog.usedFor(s.year).maxByOrNull { it.price }!!
        val quote = Actions.usedPrice(s, type.id)
        s = s.withCash("liberty", quote * 1.5)

        val tooMany = Actions.execute(s, Command.BuyAircraft("liberty", type.id, 2, used = true))
        assertFalse(tooMany.ok, "돈이 모자라면 절반만 사는 대신 실패해야 한다")

        val fits = Actions.execute(s, Command.BuyAircraft("liberty", type.id, 1, used = true))
        assertTrue(fits.ok, fits.message)
        val spent = s.airline("liberty").cash - fits.state.airline("liberty").cash
        assertTrue(abs(spent - quote) < 1.0, "화면에 뜬 값($quote)과 실제 결제액($spent)이 다르다")
    }

    // ------------------------------------------------------------ 파산 정리

    @Test
    fun `파산한 회사의 주식과 발주는 깨끗이 정리된다`() {
        var s = fresh()
        s = s.copy(orders = s.orders + Order(90002, "fuji", "b727", 2, s.turn + 2))
        s = Actions.execute(s, Command.Loan("hanseong", 200e6)).state
        s = Actions.execute(s, Command.TradeShares("hanseong", "fuji", s.airline("fuji").shares * 0.05)).state
        assertTrue((s.player.holdings["fuji"] ?: 0.0) > 0)

        // 후지에어라인을 자본잠식으로 몰아 파산시킨다.
        s = s.withAirline2("fuji") {
            it.copy(cash = -1e9, debt = 5e9, negativeQuarters = Balance.BANKRUPTCY_GRACE + 1)
        }
        s = TurnEngine.advance(s)

        assertFalse(s.airline("fuji").alive, "자본잠식이 이어졌는데 파산하지 않았다")
        assertTrue(s.orders.none { it.airlineId == "fuji" }, "파산한 회사 앞으로 발주가 남아 있다")
        assertTrue(
            s.airlines.none { it.holdings.containsKey("fuji") },
            "휴지가 된 주식이 남아 기업가치를 부풀린다",
        )
    }
}

private fun GameState.withCash(id: String, cash: Double): GameState =
    copy(airlines = airlines.map { if (it.id == id) it.copy(cash = cash) else it })

private fun GameState.withAirline2(
    id: String,
    f: (skytycoon.core.model.Airline) -> skytycoon.core.model.Airline,
): GameState = copy(airlines = airlines.map { if (it.id == id) f(it) else it })
