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
        // 네 곳만으로는 세계가 텅 빈다 — 노선이 겹치지 않아 경쟁이 안 붙고,
        // 삼킬 상대도 없어 인수합병이 아예 발동하지 않는다. 대륙마다 라이벌을 둔다.
        CompanySeed(
            id = "huanan", name = "화남항공", short = "HN", colorArgb = 0xFFE0654F,
            home = "shanghai", trait = Trait.EXPAND, cash = 380e6, shares = 50e6,
            startFleet = listOf("b727" to 3, "b737_200" to 4),
            startSlots = mapOf("shanghai" to 24, "beijing" to 10, "hongkong" to 6, "tokyo" to 4, "bangkok" to 4),
            bonusKey = "slot", bonusLabel = "국영의 뒷배",
            bonusDesc = "슬롯 매입가가 20% 싸다. 본토 요지를 쓸어담는다.",
        ),
        CompanySeed(
            id = "gulfwing", name = "걸프윙", short = "GW", colorArgb = 0xFF34B3A0,
            home = "dubai", trait = Trait.PREMIUM, cash = 520e6, shares = 58e6,
            startFleet = listOf("b707" to 4, "b727" to 3),
            startSlots = mapOf("dubai" to 26, "cairo" to 8, "delhi" to 6, "london" to 5, "istanbul" to 5),
            bonusKey = "fuel", bonusLabel = "산유국의 급유",
            bonusDesc = "장기 계약으로 연료비를 6% 절감한다. 오일쇼크가 기회가 된다.",
        ),
        CompanySeed(
            id = "condor", name = "콘도르항공", short = "CD", colorArgb = 0xFFC77DD8,
            home = "saopaulo", trait = Trait.VALUE, cash = 340e6, shares = 46e6,
            startFleet = listOf("b737_200" to 5, "dc9" to 3),
            startSlots = mapOf("saopaulo" to 22, "buenosaires" to 10, "lima" to 6, "miami" to 5, "mexicocity" to 5),
            bonusKey = "finance", bonusLabel = "원자재 재벌의 뒷돈",
            bonusDesc = "차입 한도가 40% 크고 이자율이 1%p 낮다.",
        ),
        CompanySeed(
            id = "southerncross", name = "서던크로스", short = "SX", colorArgb = 0xFF7FA8E8,
            home = "sydney", trait = Trait.BALANCED, cash = 400e6, shares = 52e6,
            startFleet = listOf("b707" to 3, "b727" to 3, "dc9" to 2),
            startSlots = mapOf("sydney" to 24, "auckland" to 10, "singapore" to 6, "honolulu" to 5, "losangeles" to 4),
            bonusKey = "service", bonusLabel = "남반구의 환대",
            bonusDesc = "창업부터 기내 서비스 등급이 한 단계 높다.",
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
            // 2028년까지 굴려야 787-10·A350-1000·A321XLR·777-9·C929 가 차례로 들어온다.
            // 2025년에 끝내면 2018년 이후 신기종의 절반을 구경도 못 하고 판이 닫힌다.
            "modern", "현대의 하늘", 2012, 17,
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
