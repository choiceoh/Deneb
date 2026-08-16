package skytycoon.core.sim

import skytycoon.core.data.Difficulties
import skytycoon.core.model.City
import skytycoon.core.model.GameState
import kotlin.math.pow
import kotlin.math.sqrt

/** 한 도시쌍의 분기 수요. 두 세그먼트는 운임 민감도가 전혀 다르다. */
data class PairDemand(val business: Double, val leisure: Double) {
    val total: Double get() = business + leisure
    val businessShare: Double get() = if (total <= 0) 0.0 else business / total
}

object Demand {
    /**
     * 도시 규모·관광 매력·거리만으로 정해지는 연간 기초 수요.
     * 비즈니스 수요는 거리에 더 빨리 감쇠하고(당일 출장이 어려워진다),
     * 레저 수요는 멀어도 비교적 버틴다.
     */
    fun annualBase(a: City, b: City, devA: Double = 1.0, devB: Double = 1.0): PairDemand {
        val d = Geo.distance(a.id, b.id)
        if (d < 1.0) return PairDemand(0.0, 0.0)

        val econA = a.econ * devA
        val econB = b.econ * devB
        val tourA = a.tour * sqrt(devA)
        val tourB = b.tour * sqrt(devB)

        val bizCore = (econA * econB).pow(Balance.DEMAND_BIZ_EXP)
        val leiCore = Balance.DEMAND_LEISURE_W * sqrt(tourA * tourB)

        val bizDecay = 1.0 / (1.0 + (d / Balance.DEMAND_DIST_HALF).pow(0.95))
        val leiDecay = 1.0 / (1.0 + (d / Balance.DEMAND_DIST_HALF).pow(0.80))

        // 가까운 도시쌍은 철도·고속도로에 승객을 뺏긴다.
        val rail = if (d < Balance.DEMAND_RAIL_RANGE) {
            (d / Balance.DEMAND_RAIL_RANGE).pow(Balance.DEMAND_RAIL_EXP)
        } else {
            1.0
        }
        val regional = if (a.region == b.region) Balance.DEMAND_SAME_REGION else 1.0
        val k = Balance.DEMAND_K * rail * regional

        return PairDemand(
            business = k * bizCore * bizDecay,
            leisure = k * leiCore * leiDecay,
        )
    }

    /** 경기·계절·도시 성장·특수 이벤트까지 반영한 이번 분기 수요. */
    fun quarterly(state: GameState, a: City, b: City): PairDemand {
        val csA = state.cityState[a.id]
        val csB = state.cityState[b.id]
        if (csA?.isClosed(state.turn) == true || csB?.isClosed(state.turn) == true) {
            return PairDemand(0.0, 0.0)
        }

        val base = annualBase(a, b, csA?.dev ?: 1.0, csB?.dev ?: 1.0)
        val q = state.quarter - 1
        val world = state.world
        val diff = Difficulties[state.difficultyId]

        val regionMul = sqrt(
            (world.regionEconomy[a.region] ?: 1.0) * (world.regionEconomy[b.region] ?: 1.0),
        )
        val boost = maxOf(csA?.boostAt(state.turn) ?: 1.0, csB?.boostAt(state.turn) ?: 1.0)
        val common = world.travelIndex * regionMul * boost * diff.demandBonus / 4.0

        // 불황이 오면 관광이 먼저 죽고 출장은 비교적 버틴다.
        val bizEcon = world.economy.pow(0.8)
        val leiEcon = world.economy.pow(1.3)

        return PairDemand(
            business = base.business * common * bizEcon * Balance.SEASON_BIZ[q],
            leisure = base.leisure * common * leiEcon * Balance.SEASON_LEISURE[q],
        )
    }

    /** 노선 계획 화면에서 쓰는 연간 환산 수요. */
    fun annualEstimate(state: GameState, a: City, b: City): Double {
        var sum = 0.0
        for (q in 0 until 4) {
            val shifted = state.copy(turn = state.turn - state.quarter + 1 + q)
            sum += quarterly(shifted, a, b).total
        }
        return sum
    }
}
