package skytycoon.core

import skytycoon.core.data.Cities
import skytycoon.core.data.Companies
import skytycoon.core.model.NewsKind
import skytycoon.core.sim.Ai
import skytycoon.core.sim.Economics
import skytycoon.core.sim.NewGame
import skytycoon.core.sim.Rng
import skytycoon.core.sim.TurnEngine
import kotlin.test.Ignore
import kotlin.test.Test

/**
 * 게임성 진단 하네스 — 자동조종 캠페인을 끝까지 굴려 "재미의 지표"를 찍는다.
 * 판정이 아니라 관찰이라 평소에는 돌리지 않는다:
 *   ./gradlew :core:jvmTest --tests '*FunDiag*' -i
 */
@Ignore
class FunDiag {
    @Test
    fun diagnose() {
        for (seed in listOf(1, 42, 777)) {
            var s = NewGame.create(seed = seed, companyId = "hanseong")
            val rng = Rng(seed * 31 + 7)
            var leadChanges = 0
            var lastLeader = ""
            val equitySeries = mutableListOf<Double>()
            var maxSlotSat = 0.0
            var idleCashPeak = 0.0
            var lossQuarters = 0
            var maxLeverage = 0.0

            repeat(s.totalTurns) {
                if (s.outcome != null) return@repeat
                s = s.copy(airlines = s.airlines) // no-op
                s = TurnEngine.advance(Ai.autoPilot(s, s.playerId, rng))
                val rank = TurnEngine.ranking(s)
                val leader = rank.firstOrNull()?.first?.name ?: ""
                if (leader != lastLeader && lastLeader.isNotEmpty()) leadChanges++
                lastLeader = leader
                equitySeries += Economics.equity(s, s.player)
                // 허브 슬롯 포화도
                val sat = Cities.all.maxOf { c ->
                    val extra = s.cityState[c.id]?.extraSlots ?: 0
                    1.0 - s.unsoldSlots(c).toDouble() / (c.slots + extra)
                }
                if (sat > maxSlotSat) maxSlotSat = sat
                for (a in s.livingAirlines) {
                    val annual = a.results.takeLast(4).sumOf { r -> r.net }
                    if (a.results.size >= 4 && annual < 0) lossQuarters++
                    val lev = a.debt / Economics.equity(s, a).coerceAtLeast(1.0)
                    if (lev > maxLeverage) maxLeverage = lev
                }
                val cashRatio = s.livingAirlines.maxOf { a ->
                    a.cash / Economics.equity(s, a).coerceAtLeast(1.0)
                }
                if (cashRatio > idleCashPeak) idleCashPeak = cashRatio
            }

            val rank = TurnEngine.ranking(s)
            val top = rank.firstOrNull()?.second ?: 0.0
            val bottom = rank.lastOrNull()?.second ?: 0.0
            val merges = s.news.count { it.headline.contains("인수 완료") }
            val bankrupt = s.news.count { it.headline.contains("파산") }
            val accidents = s.news.count { it.kind == NewsKind.ACCIDENT }
            val expansions = s.news.count { it.headline.contains("확장 완공") }
            val playerRank = rank.indexOfFirst { it.first.id == s.playerId } + 1
            println(
                "seed=$seed 결과=${s.outcome?.reason?.take(14)} 플레이어순위=$playerRank/${rank.size} " +
                    "선두교체=$leadChanges 상하격차=${(top / bottom.coerceAtLeast(1.0) * 10).toInt() / 10.0}배 " +
                    "인수=$merges 파산=$bankrupt 확장=$expansions " +
                    "현금/자본=${(idleCashPeak * 100).toInt()}% 적자회사분기=$lossQuarters 최대레버리지=${(maxLeverage * 100).toInt()}%",
            )
            val q = equitySeries.filterIndexed { i, _ -> i % 16 == 15 }
            println("   플레이어 기업가치 4년마다: " + q.joinToString(" → ") { "${(it / 1e9 * 10).toInt() / 10.0}B" })
        }
        println("회사 수=${Companies.all.size} 도시=${Cities.all.size}")
    }
}
