package skytycoon.core.data

import skytycoon.core.model.Difficulty
import skytycoon.core.model.Scenario
import skytycoon.core.model.Trait

/** 창업 시점의 항공사 설정. 고른 하나가 플레이어, 나머지는 AI 경쟁사가 된다. */
data class CompanySeed(
    val id: String,
    val name: String,
    val short: String,
    val colorArgb: Long,
    val home: String,
    val trait: Trait,
    val cash: Double,
    val shares: Double,
    val startFleet: List<Pair<String, Int>>,
    val startSlots: Map<String, Int>,
    val bonusKey: String,
    val bonusLabel: String,
    val bonusDesc: String,
)

object Companies {
    val all: List<CompanySeed> = listOf(
        CompanySeed(
            id = "hanseong", name = "한성항공", short = "HS", colorArgb = 0xFF4F8EF7,
            home = "seoul", trait = Trait.BALANCED, cash = 320e6, shares = 40e6,
            startFleet = listOf("b727" to 4, "b737_200" to 3),
            startSlots = mapOf("seoul" to 22, "tokyo" to 6, "osaka" to 4, "hongkong" to 4, "taipei" to 4),
            bonusKey = "fuel", bonusLabel = "연료 헤지",
            bonusDesc = "장기 계약으로 연료비를 6% 절감한다. 오일쇼크에 강하다.",
        ),
        CompanySeed(
            id = "fuji", name = "후지에어라인", short = "FJ", colorArgb = 0xFFE2574C,
            home = "tokyo", trait = Trait.PREMIUM, cash = 480e6, shares = 60e6,
            startFleet = listOf("b727" to 3, "b707" to 3, "dc9" to 2),
            startSlots = mapOf("tokyo" to 26, "osaka" to 12, "seoul" to 4, "honolulu" to 6, "hongkong" to 4),
            bonusKey = "service", bonusLabel = "장인의 접객",
            bonusDesc = "창업부터 기내 서비스 등급이 한 단계 높다.",
        ),
        CompanySeed(
            id = "britannia", name = "브리타니아항공", short = "BR", colorArgb = 0xFF3FB27F,
            home = "london", trait = Trait.EXPAND, cash = 420e6, shares = 55e6,
            startFleet = listOf("b707" to 4, "b727" to 2, "dc9" to 2),
            startSlots = mapOf("london" to 26, "paris" to 8, "frankfurt" to 6, "rome" to 5, "newyork" to 6),
            bonusKey = "slot", bonusLabel = "구제국의 인맥",
            bonusDesc = "슬롯 매입가가 20% 싸다. 요지를 선점하기 좋다.",
        ),
        CompanySeed(
            id = "liberty", name = "리버티에어", short = "LB", colorArgb = 0xFFD8A13A,
            home = "newyork", trait = Trait.VALUE, cash = 560e6, shares = 70e6,
            startFleet = listOf("b727" to 5, "b737_200" to 4, "b707" to 2),
            startSlots = mapOf("newyork" to 28, "chicago" to 12, "losangeles" to 8, "miami" to 6, "london" to 4),
            bonusKey = "finance", bonusLabel = "월가의 신용",
            bonusDesc = "차입 한도가 40% 크고 이자율이 1%p 낮다.",
        ),
    )

    operator fun get(id: String): CompanySeed = all.first { it.id == id }
}

object Scenarios {
    val all: List<Scenario> = listOf(
        Scenario(
            "goldenage", "제트 여명기", 1970, 20,
            "와이드바디의 등장, 두 차례의 오일쇼크, 그리고 규제 완화. 항공사가 세계 지도를 다시 그린 20년.",
        ),
        Scenario(
            "megacarrier", "메가캐리어 전쟁", 1990, 16,
            "747-400과 777의 시대. 초장거리 직항과 허브 지배력으로 승부가 갈린다.",
        ),
        Scenario(
            "modern", "현대의 하늘", 2012, 14,
            "초효율 쌍발기와 걸프 허브의 시대. 연료 효율이 곧 경쟁력이다.",
        ),
    )

    operator fun get(id: String): Scenario = all.first { it.id == id }
}

object Difficulties {
    val all: List<Difficulty> = listOf(
        Difficulty("easy", "평이", aiSkill = 0.72, demandBonus = 1.10, costMul = 0.94),
        Difficulty("normal", "보통", aiSkill = 1.00, demandBonus = 1.00, costMul = 1.00),
        Difficulty("hard", "가혹", aiSkill = 1.28, demandBonus = 0.94, costMul = 1.06),
    )

    operator fun get(id: String): Difficulty = all.first { it.id == id }
}
