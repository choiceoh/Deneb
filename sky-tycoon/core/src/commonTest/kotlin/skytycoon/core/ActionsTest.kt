package skytycoon.core

import skytycoon.core.data.Scenarios
import skytycoon.core.model.BusinessType
import skytycoon.core.model.GameState
import skytycoon.core.sim.Actions
import skytycoon.core.sim.Command
import skytycoon.core.sim.Economics
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Stock
import skytycoon.core.sim.TurnEngine
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ActionsTest {

    private fun fresh(): GameState = NewGame.create(seed = 2026, companyId = "hanseong")

    /** 배속되지 않은 기재를 하나 고른다. */
    private fun idlePlane(s: GameState, airlineId: String) =
        s.planesOf(airlineId).first { it.routeId == null }

    private fun freeUpPlane(s: GameState, airlineId: String): GameState {
        val route = s.routesOf(airlineId).first()
        return Actions.execute(s, Command.CloseRoute(airlineId, route.id)).state
    }

    @Test
    fun `항속거리가 모자라면 노선을 못 연다`() {
        var s = freeUpPlane(fresh(), "hanseong")
        // B727(4000km)로 서울-뉴욕(11000km)은 불가능하다.
        val plane = s.planesOf("hanseong").first { it.routeId == null && it.typeId == "b727" }
        val r = Actions.execute(
            s,
            Command.OpenRoute("hanseong", "seoul", "newyork", listOf(plane.id), freq = 2),
        )
        assertFalse(r.ok)
        assertTrue("항속거리" in r.message, "실패 사유가 항속거리여야 한다: ${r.message}")
    }

    @Test
    fun `슬롯이 없으면 노선을 못 연다`() {
        val s = freeUpPlane(fresh(), "hanseong")
        val plane = idlePlane(s, "hanseong")
        // 한성항공은 방콕에 슬롯이 없다.
        val r = Actions.execute(
            s,
            Command.OpenRoute("hanseong", "seoul", "bangkok", listOf(plane.id), freq = 3),
        )
        assertFalse(r.ok)
        assertTrue("슬롯" in r.message, "실패 사유가 슬롯이어야 한다: ${r.message}")
    }

    @Test
    fun `슬롯을 사면 노선을 열 수 있다`() {
        var s = freeUpPlane(fresh(), "hanseong")
        val before = s.airline("hanseong").cash
        s = Actions.execute(s, Command.BuySlots("hanseong", "bangkok", 4)).state
        assertTrue(s.airline("hanseong").cash < before, "슬롯을 샀는데 현금이 그대로다")
        assertEquals(4, s.airline("hanseong").slotsAt("bangkok"))

        val plane = s.planesOf("hanseong").first {
            it.routeId == null && Economics.canFly(
                skytycoon.core.data.AircraftCatalog[it.typeId],
                skytycoon.core.sim.Geo.distance("seoul", "bangkok"),
            )
        }
        val r = Actions.execute(
            s,
            Command.OpenRoute("hanseong", "seoul", "bangkok", listOf(plane.id), freq = 3),
        )
        assertTrue(r.ok, r.message)
    }

    @Test
    fun `기재 한 대가 감당할 수 있는 편수를 넘길 수 없다`() {
        // 서울-도쿄 노선을 일단 접어 기재와 구간을 비운다.
        var s = fresh()
        val tokyoRoute = s.routesOf("hanseong").first { it.touches("tokyo") }
        s = Actions.execute(s, Command.CloseRoute("hanseong", tokyoRoute.id)).state
        val plane = idlePlane(s, "hanseong")
        val r = Actions.execute(
            s,
            Command.OpenRoute("hanseong", "seoul", "tokyo", listOf(plane.id), freq = 99),
        )
        assertFalse(r.ok)
        assertTrue("한계" in r.message, r.message)
    }

    @Test
    fun `차입 한도를 넘길 수 없다`() {
        val s = fresh()
        val cap = Actions.debtCapacity(s, s.player)
        assertTrue(Actions.execute(s, Command.Loan("hanseong", cap * 0.5)).ok)
        assertFalse(Actions.execute(s, Command.Loan("hanseong", cap * 1.5)).ok)
    }

    @Test
    fun `발주한 기재는 두 분기 뒤에 도착한다`() {
        var s = fresh()
        val before = s.planesOf("hanseong").size
        val r = Actions.execute(s, Command.BuyAircraft("hanseong", "b727", 2))
        assertTrue(r.ok, r.message)
        s = r.state
        assertEquals(before, s.planesOf("hanseong").size, "발주 즉시 기재가 늘면 안 된다")
        repeat(2) { s = TurnEngine.advance(s) }
        assertTrue(s.planesOf("hanseong").size >= before + 2, "발주한 기재가 도착하지 않았다")
    }

    @Test
    fun `취항하지 않은 도시에는 부대사업을 못 낸다`() {
        val s = fresh()
        assertFalse(Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HOTEL, "lagos")).ok)
        // 서울은 모기지라 취항 노선이 반드시 있다.
        assertTrue(Actions.execute(s, Command.BuildBusiness("hanseong", BusinessType.HOTEL, "seoul")).ok)
    }

    @Test
    fun `한 분기에 사들일 수 있는 지분에 상한이 있다`() {
        var s = fresh()
        s = Actions.execute(s, Command.Loan("hanseong", Actions.debtCapacity(s, s.player) * 0.9)).state
        val target = s.airline("fuji")
        val tooMuch = Actions.execute(s, Command.TradeShares("hanseong", "fuji", target.shares * 0.4))
        assertFalse(tooMuch.ok, "지분 40%를 한 번에 사들일 수 있으면 안 된다")

        val ok = Actions.execute(s, Command.TradeShares("hanseong", "fuji", target.shares * 0.05))
        assertTrue(ok.ok, ok.message)
        assertTrue(Stock.ownershipRatio(ok.state, "hanseong", "fuji") > 0.04)
    }

    @Test
    fun `유상증자는 적대적 지분을 희석한다`() {
        var s = fresh()
        s = Actions.execute(s, Command.Loan("fuji", 400e6)).state
        s = Actions.execute(s, Command.TradeShares("fuji", "hanseong", s.airline("hanseong").shares * 0.09)).state
        val before = Stock.ownershipRatio(s, "fuji", "hanseong")

        val issue = Actions.execute(s, Command.IssueShares("hanseong", Actions.maxIssuable(s.player)))
        assertTrue(issue.ok, issue.message)
        val after = Stock.ownershipRatio(issue.state, "fuji", "hanseong")
        assertTrue(after < before, "증자를 했는데 지분이 희석되지 않았다 ($before → $after)")
        assertTrue(issue.state.airline("hanseong").cash > s.airline("hanseong").cash, "증자 대금이 안 들어왔다")
    }

    @Test
    fun `모든 시나리오가 끝까지 돌아간다`() {
        for (scenario in Scenarios.all) {
            var s = NewGame.create(scenarioId = scenario.id, companyId = "britannia", seed = 31)
            assertTrue(s.routes.isNotEmpty(), "${scenario.name}: 창업 노선망이 비어 있다")
            assertTrue(
                s.planes.all { skytycoon.core.data.AircraftCatalog[it.typeId].year <= s.year },
                "${scenario.name}: 아직 나오지 않은 기종을 갖고 시작한다",
            )
            repeat(s.totalTurns) { if (s.outcome == null) s = TurnEngine.advance(s) }
            println("[${scenario.name}] ${s.year}년 종료 · ${s.outcome?.reason} · 생존 ${s.livingAirlines.size}사")
            assertTrue(s.outcome != null)
        }
    }
}
