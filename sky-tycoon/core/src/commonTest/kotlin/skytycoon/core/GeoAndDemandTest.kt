package skytycoon.core

import skytycoon.core.data.Cities
import skytycoon.core.sim.Demand
import skytycoon.core.sim.Geo
import kotlin.math.abs
import kotlin.test.Test
import kotlin.test.assertTrue

class GeoAndDemandTest {

    @Test
    fun `대권거리가 실제 값과 맞는다`() {
        val pairs = listOf(
            Triple("seoul", "tokyo", 1160.0),
            Triple("newyork", "london", 5570.0),
            Triple("london", "sydney", 16990.0),
            Triple("losangeles", "newyork", 3940.0),
        )
        for ((a, b, expected) in pairs) {
            val d = Geo.distance(a, b)
            assertTrue(
                abs(d - expected) / expected < 0.03,
                "$a-$b 거리 ${d.toInt()}km 가 기대값 ${expected.toInt()}km 와 3% 넘게 다르다",
            )
        }
    }

    @Test
    fun `날짜변경선을 넘는 경로는 두 조각으로 쪼개진다`() {
        val segments = Geo.greatCirclePath(Cities["tokyo"], Cities["losangeles"])
        assertTrue(segments.size >= 2, "태평양 횡단 경로가 지도에서 끊기지 않았다")
        assertTrue(segments.all { seg -> seg.all { it.x in 0.0..1.0 && it.y in 0.0..1.0 } })
    }

    @Test
    fun `수요는 거리가 멀수록 줄고 큰 도시일수록 는다`() {
        val seoulTokyo = Demand.annualBase(Cities["seoul"], Cities["tokyo"]).total
        val seoulLima = Demand.annualBase(Cities["seoul"], Cities["lima"]).total
        assertTrue(seoulTokyo > seoulLima * 5, "가깝고 큰 시장이 훨씬 커야 한다")

        val nycLondon = Demand.annualBase(Cities["newyork"], Cities["london"]).total
        val nycLima = Demand.annualBase(Cities["newyork"], Cities["lima"]).total
        assertTrue(nycLondon > nycLima)
    }

    @Test
    fun `주요 노선 수요가 게임 스케일 안에 있다`() {
        // 간선 노선 하나가 여객기 두세 대(분기 5만 석 안팎)를 먹여 살리는 규모여야
        // 운임·편수 경쟁이 의미를 갖는다. Balance.DEMAND_K 주석 참고.
        val checks = listOf(
            Triple("seoul", "tokyo", 350_000.0..800_000.0),
            Triple("newyork", "london", 180_000.0..600_000.0),
            Triple("newyork", "losangeles", 250_000.0..700_000.0),
        )
        for ((a, b, range) in checks) {
            val d = Demand.annualBase(Cities[a], Cities[b]).total
            assertTrue(d in range, "$a-$b 연간 수요 ${d.toInt()} 명이 기대 범위 $range 밖이다")
        }

        // 변두리 노선은 간선의 몇 분의 일이어야 한다.
        val trunk = Demand.annualBase(Cities["seoul"], Cities["tokyo"]).total
        val thin = Demand.annualBase(Cities["nairobi"], Cities["lima"]).total
        assertTrue(thin < trunk / 4, "변두리 노선 수요 ${thin.toInt()} 가 간선 대비 너무 크다")
    }

    @Test
    fun `장거리 노선일수록 레저 비중이 높다`() {
        val short = Demand.annualBase(Cities["seoul"], Cities["tokyo"])
        val long = Demand.annualBase(Cities["seoul"], Cities["sydney"])
        assertTrue(
            long.businessShare < short.businessShare,
            "장거리에서 출장 비중이 더 낮아야 한다 (단거리 ${short.businessShare}, 장거리 ${long.businessShare})",
        )
    }
}
