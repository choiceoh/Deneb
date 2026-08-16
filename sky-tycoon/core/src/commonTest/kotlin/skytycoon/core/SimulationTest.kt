package skytycoon.core

import skytycoon.core.model.GameState
import skytycoon.core.save.Save
import skytycoon.core.sim.Ai
import skytycoon.core.sim.Economics
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Rng
import skytycoon.core.sim.TurnEngine
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class SimulationTest {

    private fun runYears(state: GameState, turns: Int, autoPilot: Boolean = false): GameState {
        var s = state
        repeat(turns) {
            if (s.outcome != null) return s
            if (autoPilot) {
                // 플레이어 회사를 AI 두뇌에 맡겨 "제대로 경영하면 굴러가는가"를 본다.
                s = Ai.autoPilot(s, s.playerId, Rng(s.rngState xor 0x5EED))
            }
            s = TurnEngine.advance(s)
        }
        return s
    }

    @Test
    fun `20년을 완주해도 세계가 무너지지 않는다`() {
        var s = NewGame.create(seed = 1234)
        s = runYears(s, s.totalTurns)

        println("[완주] ${s.year}년 ${s.quarter}분기, 결과=${s.outcome?.reason}")
        for ((a, equity) in TurnEngine.ranking(s)) {
            val fleet = s.planesOf(a.id).size
            val routes = s.routesOf(a.id).size
            println(
                "  ${a.name}: 기업가치 ${(equity / 1e6).toInt()}M, 기재 ${fleet}대, " +
                    "노선 ${routes}개, 현금 ${(a.cash / 1e6).toInt()}M, 부채 ${(a.debt / 1e6).toInt()}M",
            )
        }

        assertTrue(s.outcome != null, "총 턴을 다 돌았으면 결과가 나와야 한다")
        assertTrue(s.livingAirlines.isNotEmpty(), "모든 항공사가 사라졌다")
        assertTrue(s.news.size > 20, "20년 동안 뉴스가 ${s.news.size}건뿐이다")
        // 값이 발산하거나 NaN 이 되면 UI 가 통째로 깨진다.
        for (a in s.airlines) {
            assertTrue(a.cash.isFinite() && a.debt.isFinite(), "${a.name} 재무 수치가 유한하지 않다")
            assertTrue(Economics.equity(s, a).isFinite(), "${a.name} 기업가치가 유한하지 않다")
        }
    }

    @Test
    fun `제대로 경영하면 회사가 성장한다`() {
        val start = NewGame.create(seed = 99, companyId = "hanseong")
        val startEquity = Economics.equity(start, start.player)
        val end = runYears(start, 40, autoPilot = true)
        val endPlayer = end.airlineOrNull("hanseong")

        println(
            "[성장] 자동조종 10년: 기업가치 ${(startEquity / 1e6).toInt()}M → " +
                "${endPlayer?.let { (Economics.equity(end, it) / 1e6).toInt() }}M, " +
                "기재 ${end.planesOf("hanseong").size}대, 노선 ${end.routesOf("hanseong").size}개",
        )
        assertTrue(endPlayer?.alive == true, "자동조종인데 10년 만에 회사가 사라졌다")
        assertTrue(
            Economics.equity(end, endPlayer) > startEquity,
            "제대로 경영해도 기업가치가 늘지 않으면 게임이 성립하지 않는다",
        )
    }

    @Test
    fun `AI 는 스스로 노선망을 넓힌다`() {
        val start = NewGame.create(seed = 55)
        val before = start.routesOf("liberty").size
        val end = runYears(start, 24)
        val after = end.routesOf("liberty").size
        println("[AI] 리버티에어 노선 ${before}개 → ${after}개, 기재 ${end.planesOf("liberty").size}대")
        assertTrue(after > before, "AI 가 6년 동안 노선을 하나도 늘리지 않았다")
    }

    @Test
    fun `같은 시드는 같은 결과를 낸다`() {
        val a = runYears(NewGame.create(seed = 4242), 20)
        val b = runYears(NewGame.create(seed = 4242), 20)
        assertEquals(Save.encode(a), Save.encode(b), "같은 시드인데 진행 결과가 갈렸다")
    }

    @Test
    fun `세이브를 저장하고 불러오면 그대로다`() {
        val s = runYears(NewGame.create(seed = 7), 9)
        val text = Save.encode(s)
        val restored = Save.decode(text)
        assertEquals(s, restored, "세이브 왕복에서 상태가 달라졌다")
        assertEquals(Save.encode(TurnEngine.advance(s)), Save.encode(TurnEngine.advance(restored)))
    }
}
