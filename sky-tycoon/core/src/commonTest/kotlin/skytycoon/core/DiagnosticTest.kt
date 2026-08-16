package skytycoon.core

import skytycoon.core.sim.Ai
import skytycoon.core.sim.Economics
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Rng
import skytycoon.core.sim.TurnEngine

import kotlin.test.Ignore
import kotlin.test.Test

/**
 * 밸런스를 눈으로 보는 진단용. 평소에는 건너뛴다.
 * 확인할 때만: `./gradlew :core:jvmTest --tests "*Diagnostic*" -Pdiag=1`
 */
@Ignore

class DiagnosticTest {

    @Test
    fun `자동조종 플레이어의 분기별 손익을 찍어본다`() {
        var s = NewGame.create(seed = 99, companyId = "hanseong")
        repeat(40) {
            if (s.outcome != null) return@repeat
            s = Ai.autoPilot(s, s.playerId, Rng(s.rngState xor 0x5EED))
            s = TurnEngine.advance(s)
            val a = s.airlineOrNull("hanseong") ?: return@repeat
            val r = a.lastResult
            if (r != null) {
                println(
                    "${s.year}Q${s.quarter} 매출 ${(r.totalRevenue / 1e6).toInt()}M " +
                        "운항비 ${((r.fuelCost + r.crewCost + r.maintCost + r.landingCost + r.paxServiceCost + r.distributionCost) / 1e6).toInt()}M " +
                        "간접 ${(r.overhead / 1e6).toInt()}M 상각 ${(r.depreciation / 1e6).toInt()}M " +
                        "이자 ${(r.interestCost / 1e6).toInt()}M → 순익 ${(r.net / 1e6).toInt()}M | " +
                        "현금 ${(a.cash / 1e6).toInt()}M 부채 ${(a.debt / 1e6).toInt()}M " +
                        "자본 ${(Economics.equity(s, a) / 1e6).toInt()}M | " +
                        "기재 ${s.planesOf("hanseong").size} 노선 ${s.routesOf("hanseong").size} " +
                        "탑승률 ${(r.loadFactor * 100).toInt()}%",
                )
            }
        }
    }
}
