package skytycoon.core

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.data.Scenarios
import skytycoon.core.model.Business
import skytycoon.core.model.BusinessType
import skytycoon.core.model.CityState
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Order
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Balance
import skytycoon.core.sim.Command
import skytycoon.core.sim.Demand
import skytycoon.core.sim.Economics
import skytycoon.core.sim.Events
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Rng
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

    // ------------------------------------------------------------ 차익거래 차단

    @Test
    fun `중고기를 샀다가 즉시 되팔면 손해다`() {
        // 매각가가 중고 매입가보다 높으면 사고파는 것만으로 현금을 무한히 찍어낼 수 있다.
        assertTrue(
            Balance.SELL_PENALTY < Balance.USED_PRICE_MUL,
            "매각가 배율(${Balance.SELL_PENALTY})이 중고 매입가 배율(${Balance.USED_PRICE_MUL}) 이상이면 돈복사가 된다",
        )

        var s = NewGame.create(scenarioId = "modern", companyId = "liberty", seed = 21)
        val type = AircraftCatalog.usedFor(s.year).first()
        val before = s.airline("liberty").cash

        val bought = Actions.execute(s, Command.BuyAircraft("liberty", type.id, 1, used = true))
        assertTrue(bought.ok, bought.message)
        s = bought.state
        val plane = s.planesOf("liberty").first { it.routeId == null && it.typeId == type.id }
        s = Actions.execute(s, Command.SellAircraft("liberty", plane.id)).state

        assertTrue(
            s.airline("liberty").cash < before,
            "중고기를 사서 바로 팔았는데 현금이 늘었다 (무한 차익거래)",
        )
    }

    // ------------------------------------------------------------ 합병 회계

    @Test
    fun `인수당한 회사가 들고 있던 우리 주식은 소각된다`() {
        var s = fresh()
        val mine = s.airline("hanseong").shares
        val stake = mine * 0.08
        s = Actions.execute(s, Command.Loan("fuji", 300e6)).state
        s = Actions.execute(s, Command.TradeShares("fuji", "hanseong", stake)).state

        s = Stock.merge(s, "hanseong", "fuji")
        assertTrue(
            s.airline("hanseong").shares < mine,
            "상대가 들고 있던 자사주가 그냥 사라졌다 (발행 주식 수 그대로)",
        )
        assertTrue(s.airlines.none { it.holdings.containsKey("hanseong") })
    }

    // ------------------------------------------------------------ 이벤트 반영

    @Test
    fun `한쪽 도시만 수요가 꺾여도 그 노선에 반영된다`() {
        val s = fresh()
        val a = skytycoon.core.data.Cities["hongkong"]
        val b = skytycoon.core.data.Cities["seoul"]
        val before = skytycoon.core.sim.Demand.quarterly(s, a, b).total

        // 사스처럼 한쪽 도시만 수요가 반 토막 나는 이벤트.
        val hit = s.copy(
            cityState = s.cityState + (a.id to (s.cityState[a.id] ?: CityState())
                .copy(boost = 0.45, boostUntilTurn = s.turn + 2)),
        )
        val after = skytycoon.core.sim.Demand.quarterly(hit, a, b).total
        assertTrue(after < before * 0.6, "한쪽만 꺾인 이벤트가 반대쪽 1.0 에 가려 사라졌다")
    }

    // ------------------------------------------------------------ 결산 리포트

    @Test
    fun `결산 리포트의 기말 잔고가 실제 잔고와 일치한다`() {
        // 현금이 마르면 결산 뒤에 긴급 차입·기재 매각이 일어난다. 리포트가 그 이전 값을 들고 있으면
        // 화면마다 다른 숫자가 보인다.
        var s = fresh()
        s = s.copy(airlines = s.airlines.map { if (it.id == "hanseong") it.copy(cash = -5e6) else it })
        s = TurnEngine.advance(s)

        val player = s.airline("hanseong")
        val report = player.lastResult!!
        assertEquals(player.cash, report.cash, "리포트의 기말 현금이 실제와 다르다")
        assertEquals(player.debt, report.debt, "리포트의 기말 부채가 실제와 다르다")
    }

    // ------------------------------------------------------------ 주가 갱신

    @Test
    fun `합병 뒤 주가가 낡은 값으로 남지 않는다`() {
        var s = fresh()
        // 후지에어라인을 인수해 대차대조표를 크게 바꾼다.
        // 상대는 유상증자(최대 30%)로 방어하므로, 희석 뒤에도 과반이 남을 만큼 쥐고 시작한다.
        s = s.copy(
            airlines = s.airlines.map {
                if (it.id == "hanseong") {
                    it.copy(holdings = it.holdings + ("fuji" to s.airline("fuji").shares * 0.85))
                } else {
                    it
                }
            },
        )
        s = TurnEngine.advance(s)
        assertFalse(s.airline("fuji").alive, "증자로도 못 막을 지분인데 인수가 성사되지 않았다")

        val player = s.airline("hanseong")
        val repriced = Stock.price(s, player)
        assertTrue(
            abs(player.sharePrice - repriced) / repriced.coerceAtLeast(1.0) < 0.02,
            "합병으로 자산이 바뀌었는데 주가가 갱신되지 않았다 (표시 ${player.sharePrice}, 실제 $repriced)",
        )
    }

    // ------------------------------------------------------------ 일시 비용

    @Test
    fun `파업 합의금은 순익과 현금에 함께 반영된다`() {
        // 현금에서 몰래 빼면 리포트의 순익과 잔고 변동이 어긋나 플레이어가 원인을 못 찾는다.
        val base = fresh()
        val charge = 20e6
        val hit = base.withAirline2("hanseong") { it.copy(pendingCharges = charge) }

        val clean = TurnEngine.advance(base).airline("hanseong")
        val struck = TurnEngine.advance(hit).airline("hanseong")

        assertEquals(0.0, clean.lastResult!!.extraordinaryCost)
        assertEquals(charge, struck.lastResult!!.extraordinaryCost)

        val netGap = clean.lastResult!!.net - struck.lastResult!!.net
        assertTrue(netGap > charge * 0.5, "합의금 ${charge}가 순익에 반영되지 않았다 (차이 $netGap)")
        assertTrue(
            abs((clean.cash - struck.cash) - netGap) < 1.0,
            "순익 감소분과 현금 감소분이 다르다 (현금 ${clean.cash - struck.cash}, 순익 $netGap)",
        )
        assertEquals(0.0, struck.pendingCharges, "털어내지 않으면 다음 분기에 또 청구된다")
    }

    // ------------------------------------------------------------ 사고 대상

    @Test
    fun `노선에 배속되지 않은 기재는 사고를 내지 않는다`() {
        var s = fresh()
        // 플레이어 기재를 전부 계류시키고 아주 낡게 만든다 — 뜨지 않으니 사고가 날 수 없다.
        s = s.copy(
            planes = s.planes.map {
                if (it.airlineId == "hanseong") it.copy(routeId = null, ageQuarters = 400) else it
            },
            routes = s.routes.map { if (it.airlineId == "hanseong") it.copy(planeIds = emptyList()) else it },
        )
        assertNoPlayerAccident(s, "격납고에 세워둔 비행기가 착륙 사고를 냈다")
    }

    @Test
    fun `운휴 노선에 남겨둔 기재도 사고를 내지 않는다`() {
        var s = fresh()
        // 배속은 그대로 두고 노선만 세운다 — 주 0편이면 뜨는 비행기가 없다.
        s = s.copy(
            planes = s.planes.map { if (it.airlineId == "hanseong") it.copy(ageQuarters = 400) else it },
            routes = s.routes.map {
                if (it.airlineId == "hanseong") it.copy(active = false, freq = 0) else it
            },
        )
        assertNoPlayerAccident(s, "운휴시킨 노선의 기재가 착륙 사고를 냈다")
    }

    /** 200분기를 굴려도 플레이어 사고가 없는지 — 다른 회사 사고는 나야 테스트가 헛돌지 않는다. */
    private fun assertNoPlayerAccident(state: GameState, why: String) {
        val playerName = state.player.name
        val rng = Rng(12345)
        var t = state
        repeat(200) { t = Events.fire(t, rng) }

        val accidents = t.news.filter { it.kind == NewsKind.ACCIDENT }
        assertTrue(accidents.isNotEmpty(), "200분기를 굴렸는데 사고가 한 건도 없다 — 테스트가 헛돌고 있다")
        assertTrue(accidents.none { it.headline.startsWith(playerName) }, why)
    }

    // ------------------------------------------------------------ 마지막 분기

    @Test
    fun `마지막 분기를 넘길 때는 다음 해 물가가 얹히지 않는다`() {
        val s = fresh().let { it.copy(turn = it.totalTurns - 1) }
        val inflation = s.world.inflation
        val end = TurnEngine.advance(s)

        assertTrue(end.outcome != null, "마지막 분기를 넘겼는데 판이 끝나지 않았다")
        assertEquals(
            inflation,
            end.world.inflation,
            "플레이하지도 않을 해의 물가가 최종 기업가치 순위에 얹혔다",
        )
    }

    @Test
    fun `끝난 판은 마지막으로 진행한 분기로 표시된다`() {
        val s = fresh().let { it.copy(turn = it.totalTurns - 1) }
        val end = TurnEngine.advance(s)

        assertEquals(s.endYear, end.displayYear, "종료 화면이 플레이하지도 않은 다음 해로 뜬다")
        assertEquals(4, end.displayQuarter, "종료 화면이 4분기가 아닌 1분기로 뜬다")
        // 진행 중일 때는 그냥 현재 분기다.
        assertEquals(s.year, s.displayYear)
        assertEquals(s.quarter, s.displayQuarter)
    }

    // ------------------------------------------------------------ 탈락 순위

    @Test
    fun `탈락 순위는 살아남은 회사 뒤에 매긴다`() {
        var s = fresh()
        // 플레이어와 한 곳만 남기고 나머지를 지운다 — 1:1 승부에서 지면 2위여야 한다.
        val survivor = s.livingAirlines.first { it.id != "hanseong" }.id
        s = s.copy(
            airlines = s.airlines.map {
                if (it.id == "hanseong" || it.id == survivor) it else it.copy(alive = false)
            },
        )
        s = s.withAirline2("hanseong") { it.copy(alive = false, mergedInto = survivor) }

        val end = TurnEngine.evaluateOutcome(s)
        val outcome = requireNotNull(end.outcome) { "회사가 사라졌는데 판이 끝나지 않았다" }
        assertFalse(outcome.won)
        assertEquals(2, outcome.rank, "이미 망한 경쟁사까지 우리 위로 세어졌다")
    }

    // ------------------------------------------------------------ 중고 매물 기령

    @Test
    fun `단종된 기종의 중고는 단종 이후로 젊을 수 없다`() {
        val s = fresh()
        for (type in AircraftCatalog.all) {
            for (year in listOf(1975, 1990, 2005, 2020)) {
                val t = s.copy(turn = (year - s.startYear) * 4)
                if (t.year <= type.year + 2) continue
                val age = Actions.usedAge(t, type.id)
                val built = t.year - age / 4
                assertTrue(
                    built <= type.retire,
                    "${type.name}(${type.year}–${type.retire} 생산)이 ${t.year}년 매물에서 ${built}년식으로 나왔다",
                )
                assertTrue(built >= type.year, "${type.name}이 취항 전인 ${built}년식으로 나왔다")
            }
        }
    }

    // ------------------------------------------------------------ 즉시 승리

    @Test
    fun `마지막 경쟁사를 인수하면 그 자리에서 판이 끝난다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val last = rivals.last()
        // 나머지는 이미 과반, 마지막 한 곳만 45% — 이번 매수가 판을 끝낸다.
        s = s.withAirline2("hanseong") { p ->
            p.copy(
                cash = 5e9,
                holdings = rivals.associate { it.id to it.shares * (if (it.id == last.id) 0.45 else 0.60) },
            )
        }

        val r = Actions.execute(s, Command.TradeShares("hanseong", last.id, last.shares * 0.06))
        assertTrue(r.ok, r.message)
        assertTrue(
            r.state.outcome?.won == true,
            "경쟁사를 모두 흡수했는데 승리 선언이 다음 턴으로 미뤄졌다",
        )
    }

    // ------------------------------------------------------------ 합병 회계

    @Test
    fun `소수 주주 정리 대금은 인수사가 낸다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val victim = rivals[0]
        val minority = rivals[1]

        // 제3자가 피인수사 지분 20% 를 들고 있는 상태에서 우리가 과반을 쥔다.
        s = s.withAirline2(minority.id) {
            it.copy(holdings = it.holdings + (victim.id to victim.shares * 0.20))
        }
        s = s.withAirline2("hanseong") {
            it.copy(holdings = it.holdings + (victim.id to victim.shares * 0.60))
        }

        val before = s.livingAirlines.sumOf { it.cash }
        // 시장에 풀린 물량은 게임 밖 주주 몫이라 그 값만큼은 인게임 현금에서 빠져나간다.
        val floatValue = Stock.floatShares(s, victim.id) * s.airline(victim.id).sharePrice
        assertTrue(floatValue > 0.0, "유통물량이 없다 — 테스트가 헛돈다")

        val after = Stock.merge(s, "hanseong", victim.id)
        val total = after.airlines.filter { it.alive }.sumOf { it.cash }

        assertTrue(
            abs((before - total) - floatValue) < 1.0,
            "현금 변동 ${before - total} 이 유통물량 값 $floatValue 과 다르다 — 어딘가에서 돈이 새거나 생겼다",
        )
        assertTrue(
            (after.airline(minority.id).cash) > minority.cash,
            "소수 주주가 지분 대금을 못 받았다",
        )
    }

    @Test
    fun `인수 대금은 자기 지분을 뺀 전부에 매겨진다`() {
        // 과반만 사고 나머지를 값도 안 치르고 소각하면 회사를 절반 값에 가져가게 된다.
        var s = fresh()
        val victimId = s.livingAirlines.first { it.id != "hanseong" }.id
        val victim = s.airline(victimId)
        s = s.withAirline2("hanseong") {
            it.copy(holdings = it.holdings + (victimId to victim.shares * 0.55))
        }

        val payout = Stock.minorityStakeValue(s, "hanseong", victimId)
        val restOfCompany = victim.shares * 0.45 * victim.sharePrice
        assertTrue(
            abs(payout - restOfCompany) < victim.sharePrice,
            "정리 대금 $payout 이 남은 지분 45% 의 값 $restOfCompany 과 다르다",
        )
    }

    @Test
    fun `합병으로 같은 도시에 같은 시설이 둘 생기지 않는다`() {
        var s = fresh()
        val victim = s.livingAirlines.first { it.id != "hanseong" }
        val city = s.player.home
        val dup = listOf(
            Business(BusinessType.HOTEL, city),
            Business(BusinessType.HANGAR, city),
        )
        s = s.withAirline2("hanseong") { it.copy(businesses = dup) }
        s = s.withAirline2(victim.id) {
            it.copy(
                businesses = dup + Business(BusinessType.HANGAR, victim.home),
                holdings = emptyMap(),
            )
        }

        val merged = Stock.merge(s, "hanseong", victim.id).airline("hanseong").businesses
        assertEquals(
            merged.size,
            merged.distinctBy { it.type to it.city }.size,
            "같은 도시에 같은 시설이 둘 남아 수입이 이중으로 잡힌다",
        )
        assertEquals(
            1,
            merged.count { it.type == BusinessType.HANGAR },
            "정비창은 회사에 하나여야 하는데 합병으로 늘어났다",
        )
    }

    @Test
    fun `인수 대금이 모자라면 현금을 마이너스로 두지 않고 차입한다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val victimId = rivals[0].id
        val minorityId = rivals[1].id

        s = s.withAirline2(minorityId) {
            it.copy(holdings = it.holdings + (victimId to s.airline(victimId).shares * 0.45))
        }
        s = s.withAirline2(victimId) { it.copy(cash = 0.0) }
        // 지분값에 비해 현금이 턱없이 모자란 인수사.
        s = s.withAirline2("hanseong") {
            it.copy(cash = 1e6, holdings = it.holdings + (victimId to s.airline(victimId).shares * 0.55))
        }

        val buyer = s.airline("hanseong")
        val victim = s.airline(victimId)
        val payout = victim.shares * 0.45 * victim.sharePrice
        val after = Stock.merge(s, "hanseong", victimId).airline("hanseong")

        assertTrue(payout > buyer.cash, "테스트가 헛돈다 — 인수 대금이 현금보다 적다")
        assertTrue(after.cash >= 0.0, "인수 직후 현금이 ${after.cash} 로 마이너스가 됐다")
        assertTrue(
            abs(after.debt - (buyer.debt + victim.debt + (payout - buyer.cash - victim.cash))) < 1.0,
            "모자란 인수 대금이 부채로 잡히지 않았다",
        )
    }

    @Test
    fun `합병에서 처분한 중복 시설은 매각대로 돌아온다`() {
        var s = fresh()
        val victimId = s.livingAirlines.first { it.id != "hanseong" }.id
        val hotel = Business(BusinessType.HOTEL, s.player.home)
        // 양쪽이 같은 도시에 같은 호텔을 가진 상태 — 하나는 처분된다.
        // 현금이 0 으로 깎이면 매각대가 묻히므로 넉넉히 쥐여 준다.
        s = s.withAirline2("hanseong") {
            it.copy(cash = 1e11, businesses = listOf(hotel), holdings = emptyMap())
        }
        s = s.withAirline2(victimId) { it.copy(businesses = listOf(hotel), holdings = emptyMap()) }

        val buyer = s.airline("hanseong")
        val victim = s.airline(victimId)
        val payout = Stock.minorityStakeValue(s, "hanseong", victimId)
        val disposal = Economics.businessValue(s, listOf(hotel))
        val after = Stock.merge(s, "hanseong", victimId).airline("hanseong")

        assertEquals(1, after.businesses.size, "중복 시설이 그대로 남았다")
        assertTrue(after.cash > 0.0, "현금이 바닥나 매각대를 확인할 수 없다 — 테스트가 헛돈다")
        assertTrue(
            abs(after.cash - (buyer.cash + victim.cash + disposal - payout)) < 1.0,
            "겹친 시설이 매각대 한 푼 없이 사라졌다",
        )
    }

    @Test
    fun `잔여 지분 정리 대금까지 못 내면 인수 매수가 막힌다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val victimId = rivals[0].id
        val minorityId = rivals[1].id
        val victimShares = s.airline(victimId).shares

        // 제3자가 45%, 우리가 48% — 3% 만 더 사면 인수가 성립한다.
        s = s.withAirline2(minorityId) {
            it.copy(holdings = it.holdings + (victimId to victimShares * 0.45))
        }
        s = s.withAirline2(victimId) { it.copy(cash = 0.0) }
        val block = victimShares * 0.03
        s = s.withAirline2("hanseong") {
            it.copy(holdings = it.holdings + (victimId to victimShares * 0.48))
        }
        // 매수 대금은 낼 수 있지만 정리 대금까지는 어림도 없는 현금·차입 여력.
        // 매집 단가는 지분율에 따라 오르므로 지분을 올린 **뒤에** 견적을 내야 한다.
        val blockCost = Stock.buyCost(s, "hanseong", victimId, block)
        s = s.withAirline2("hanseong") {
            it.copy(cash = blockCost * 1.05, debt = Actions.debtCapacity(s, it))
        }

        val r = Actions.execute(s, Command.TradeShares("hanseong", victimId, block))
        assertFalse(r.ok, "정리 대금을 못 내는데도 인수가 성립했다")
        assertTrue(r.message.contains("잔여 지분"), "실패 이유가 정리 대금이라고 알려주지 않는다: ${r.message}")
    }

    @Test
    fun `화면이 제안한 매수량은 실행 단계에서 튕기지 않는다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val victimId = rivals[0].id
        val minorityId = rivals[1].id
        val shares = s.airline(victimId).shares

        // 정리 대금이 큰 판 — 제3자가 45%, 우리가 48% 를 쥐고 있다.
        s = s.withAirline2(minorityId) { it.copy(holdings = it.holdings + (victimId to shares * 0.45)) }
        s = s.withAirline2(victimId) { it.copy(cash = 0.0) }
        s = s.withAirline2("hanseong") { it.copy(holdings = it.holdings + (victimId to shares * 0.48)) }

        // 현금이 빠듯한 구간부터 넉넉한 구간까지 — 버튼이 내놓은 값은 언제나 실행돼야 한다.
        var everOffered = false
        for (cash in listOf(1e7, 5e7, 2e8, 1e9, 1e10)) {
            val t = s.withAirline2("hanseong") { it.copy(cash = cash) }
            val offer = Stock.affordableShares(t, "hanseong", victimId)
            if (offer <= 0.0) continue
            everOffered = true
            val r = Actions.execute(t, Command.TradeShares("hanseong", victimId, offer))
            assertTrue(r.ok, "현금 $cash 에서 제안한 물량이 튕겼다: ${r.message}")
        }
        assertTrue(everOffered, "어느 현금 구간에서도 매수를 제안하지 않았다 — 테스트가 헛돈다")
    }

    @Test
    fun `대출 한도까지 당겨 쓴 회사도 소수 지분은 살 수 있다`() {
        // 인수가 아닌 평범한 매수까지 차입 여력으로 막으면 안 된다 — 빚이 늘지 않는 거래다.
        var s = fresh()
        val targetId = s.livingAirlines.first { it.id != "hanseong" }.id
        s = s.withAirline2("hanseong") {
            it.copy(cash = 5e8, debt = Actions.debtCapacity(s, it) * 1.2)
        }

        val block = s.airline(targetId).shares * 0.02
        assertTrue(
            Stock.canAfford(s, "hanseong", targetId, block),
            "빚이 늘지도 않는 소수 지분 매수를 차입 여력으로 막았다",
        )
        val r = Actions.execute(s, Command.TradeShares("hanseong", targetId, block))
        assertTrue(r.ok, r.message)
        assertTrue(Stock.affordableShares(s, "hanseong", targetId) > 0.0, "버튼이 아무 물량도 제안하지 않는다")
    }

    @Test
    fun `연쇄 인수로 물려받는 빚까지 함께 따진다`() {
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        val firstId = rivals[0].id
        val secondId = rivals[1].id
        val thirdId = rivals[2].id

        // first 가 second 의 과반을 쥐고 있다 — first 를 삼키면 second 까지 연쇄로 넘어온다.
        s = s.withAirline2(firstId) {
            it.copy(holdings = it.holdings + (secondId to s.airline(secondId).shares * 0.60))
        }
        // third 가 second 지분 35% 를 들고 있어 연쇄 인수에 큰 정리 대금이 붙는다.
        s = s.withAirline2(thirdId) {
            it.copy(holdings = it.holdings + (secondId to s.airline(secondId).shares * 0.35))
        }
        s = s.withAirline2(secondId) { it.copy(cash = 0.0) }
        s = s.withAirline2("hanseong") {
            it.copy(cash = 1e10, holdings = it.holdings + (firstId to s.airline(firstId).shares * 0.48))
        }

        val block = s.airline(firstId).shares * 0.03
        val r = Actions.execute(s, Command.TradeShares("hanseong", firstId, block))
        assertTrue(r.ok, r.message)

        assertFalse(r.state.airline(firstId).alive, "과반을 넘겼는데 인수가 성립하지 않았다")
        assertFalse(r.state.airline(secondId).alive, "연쇄 인수가 일어나지 않아 테스트가 헛돈다")

        val me = r.state.airline("hanseong")
        assertTrue(me.cash >= 0.0, "연쇄 인수로 현금이 ${me.cash} 가 됐다")
        assertTrue(
            me.debt <= Actions.debtCapacity(r.state, me) + 1.0,
            "연쇄 인수로 차입 여력(${Actions.debtCapacity(r.state, me)})을 넘는 빚 ${me.debt} 를 떠안았다",
        )
    }

    @Test
    fun `파산한 회사의 부대사업은 자산으로 남지 않는다`() {
        var s = fresh()
        val doomedId = s.livingAirlines.first { it.id != "hanseong" }.id
        s = s.withAirline2(doomedId) {
            it.copy(
                businesses = listOf(Business(BusinessType.HOTEL, it.home)),
                cash = -1e9,
                debt = 5e9,
                negativeQuarters = Balance.BANKRUPTCY_GRACE + 1,
            )
        }
        s = TurnEngine.advance(s)

        val dead = s.airline(doomedId)
        assertFalse(dead.alive, "자본잠식이 이어졌는데 파산하지 않았다")
        assertTrue(dead.businesses.isEmpty(), "파산했는데 호텔이 남아 자산으로 잡힌다")
        assertTrue(
            Economics.equity(s, dead) <= 0.0,
            "파산한 회사의 기업가치가 ${Economics.equity(s, dead)} 로 플러스다",
        )
    }

    // ------------------------------------------------------------ 일시 효과 중첩

    @Test
    fun `일시 수요 효과는 서로를 덮어쓰지 않는다`() {
        var s = fresh()
        // 2008년 3분기 — 베이징 올림픽(배율 2.0)이 대본으로 걸리는 분기.
        s = s.copy(turn = (2008 - s.startYear) * 4 + 2)
        // 그 위에 이미 수요를 반토막 낸 효과가 걸려 있다.
        s = s.copy(
            cityState = s.cityState + (
                "beijing" to (s.cityState["beijing"] ?: CityState())
                    .copy(boost = 0.5, boostUntilTurn = s.turn + 1)
                ),
        )

        val t = Events.fire(s, Rng(1))
        val boost = t.cityState["beijing"]!!.boostAt(t.turn)

        assertTrue(boost < 2.0, "폭락을 지우고 올림픽 부스트가 통째로 덮였다 (배율 $boost)")
        assertTrue(boost > 0.5, "올림픽이 아예 반영되지 않았다 (배율 $boost)")
    }

    @Test
    fun `짧은 폐쇄가 긴 폐쇄를 앞당겨 끝내지 않는다`() {
        var s = fresh()
        // 1991년 1분기 — 걸프전 대본이 텔아비브를 두 분기 닫는 분기.
        s = s.copy(turn = (1991 - s.startYear) * 4)
        // 그보다 긴 폐쇄가 이미 걸려 있다면 대본이 그걸 줄여선 안 된다.
        val longUntil = s.turn + 5
        s = s.copy(
            cityState = s.cityState + (
                "telaviv" to (s.cityState["telaviv"] ?: CityState()).copy(closedUntilTurn = longUntil)
                ),
        )

        val t = Events.fire(s, Rng(99))
        assertEquals(
            longUntil,
            t.cityState["telaviv"]!!.closedUntilTurn,
            "짧은 폐쇄가 덮어써서 공항이 일찍 열렸다",
        )
    }

    @Test
    fun `명령을 실행하면 주가가 곧바로 다시 매겨진다`() {
        // 매도 수수료·증자·기재 매입 어느 쪽이든 자기자본이 바뀌면 주가도 바뀌어야 한다.
        var s = fresh()
        val targetId = s.livingAirlines.first { it.id != "hanseong" }.id
        s = s.withAirline2("hanseong") {
            it.copy(cash = 5e9, holdings = it.holdings + (targetId to s.airline(targetId).shares * 0.20))
        }
        s = Stock.repriceAll(s)
        val before = s.player.sharePrice

        val r = Actions.execute(s, Command.TradeShares("hanseong", targetId, -(s.player.holdings[targetId]!!)))
        assertTrue(r.ok, r.message)
        assertTrue(
            r.state.player.sharePrice != before,
            "지분을 전량 털었는데 주가가 $before 그대로다 (매도 수수료만큼 자기자본이 줄었다)",
        )
    }

    @Test
    fun `주가 재산정은 교차 보유까지 반영해 수렴한다`() {
        // 한 패스만 돌면 남의 주식 값이 재산정 이전 시세로 굳어, 한 번 더 돌릴 때마다
        // 값이 계속 움직인다 — 그러면 같은 분기에 낡은 시세로 거래하게 된다.
        var s = fresh()
        val rivals = s.livingAirlines.filter { it.id != "hanseong" }
        s = s.withAirline2("hanseong") {
            it.copy(holdings = it.holdings + (rivals[0].id to rivals[0].shares * 0.40))
        }
        s = s.withAirline2(rivals[0].id) {
            it.copy(holdings = it.holdings + (rivals[1].id to rivals[1].shares * 0.30))
        }

        val once = Stock.repriceAll(s)
        val twice = Stock.repriceAll(once)
        for (a in once.airlines) {
            val again = twice.airline(a.id).sharePrice
            assertTrue(
                abs(a.sharePrice - again) <= a.sharePrice * 1e-6,
                "${a.name} 주가가 재산정을 또 돌리자 ${a.sharePrice} → $again 로 바뀐다 (수렴 안 함)",
            )
        }
    }

    // ------------------------------------------------------------ AI 견적

    @Test
    fun `AI 슬롯 견적은 매입할 때와 같은 누적 단가를 쓴다`() {
        val s = fresh()
        val city = s.routesOf("hanseong").first().from
        // 한 개 단가 × 개수로 잡으면 실제보다 싸다 — 살수록 값이 오르기 때문.
        val naive = Economics.slotPrice(s, "hanseong", city) * 5
        val real = Actions.slotCost(s, "hanseong", city, 5)
        assertTrue(real > naive, "슬롯값이 매입할수록 오르지 않는다 — 테스트가 헛돈다")

        // 그리고 매입은 정확히 slotCost 만큼만 청구해야 한다.
        val before = s.player.cash
        val r = Actions.execute(s, Command.BuySlots("hanseong", city, 5))
        assertTrue(r.ok, r.message)
        assertTrue(
            abs((before - r.state.player.cash) - real) < 1.0,
            "견적 $real 과 실제 청구 ${before - r.state.player.cash} 가 다르다",
        )
    }

    // ------------------------------------------------------------ 초기 기재·수요 추정

    @Test
    fun `시작 기재는 취항 전에 만들어지지 않는다`() {
        for (scenario in Scenarios.all) {
            val s = NewGame.create(scenarioId = scenario.id, seed = 7, companyId = "hanseong")
            for (p in s.planes) {
                val type = AircraftCatalog[p.typeId]
                val built = s.startYear - p.ageQuarters / 4
                assertTrue(
                    built >= type.year,
                    "${type.name}(${type.year} 취항)이 ${s.startYear}년 시작 기재에서 ${built}년식으로 나왔다",
                )
            }
        }
    }

    @Test
    fun `연간 수요 추정은 지난 분기에 일시 효과를 소급하지 않는다`() {
        val base = fresh()
        val a = Cities["seoul"]
        val b = Cities["tokyo"]

        // 3분기로 옮긴 뒤, 이번 분기에만 걸리는 폐쇄를 넣는다.
        var s = base.copy(turn = base.turn + 2)
        val plain = Demand.annualEstimate(s, a, b)
        s = s.copy(
            cityState = s.cityState + ("seoul" to (s.cityState["seoul"] ?: CityState()).copy(closedUntilTurn = s.turn)),
        )
        val withClosure = Demand.annualEstimate(s, a, b)

        assertTrue(withClosure > 0.0, "한 분기 폐쇄가 연간 수요를 통째로 0 으로 만들었다")
        assertTrue(withClosure < plain, "폐쇄가 연간 추정에 전혀 반영되지 않았다")
        assertTrue(
            withClosure > plain * 0.5,
            "한 분기 폐쇄인데 연간 추정이 절반 넘게 깎였다 (지난 분기까지 소급됐다)",
        )
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
