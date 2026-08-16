package skytycoon.core.model

import kotlinx.serialization.Serializable

// ---------------------------------------------------------------------------
// 정적 마스터 데이터 (세이브에 들어가지 않고 id 로만 참조된다)
// ---------------------------------------------------------------------------

enum class Region(val label: String, val colorArgb: Long) {
    AS("아시아", 0xFFE8734A),
    ME("중동", 0xFFD4A13A),
    EU("유럽", 0xFF5B8FF9),
    NA("북미", 0xFF6DC8A0),
    SA("남미", 0xFFB681E0),
    AF("아프리카", 0xFFD4694A),
    OC("오세아니아", 0xFF4EC4D3),
}

/**
 * @param econ 경제 규모 지수 — 비즈니스 수요를 만든다
 * @param tour 관광 매력 지수 — 레저 수요를 만든다
 * @param slots 주간 총 슬롯. 모든 항공사가 나눠 가지므로 허브일수록 쟁탈전이 벌어진다
 * @param fee 착륙료 지수 (1.0 = 표준)
 * @param growth 연간 도시 성장 배율
 */
data class City(
    val id: String,
    val name: String,
    val code: String,
    val lat: Double,
    val lon: Double,
    val region: Region,
    val econ: Double,
    val tour: Double,
    val slots: Int,
    val fee: Double,
    val growth: Double,
)

/**
 * @param range 페이로드 만재 기준 항속거리 (km)
 * @param fuel 연료 소모 (L/km)
 * @param maint 정비비 (USD/비행시간)
 * @param crew 승무원비 (USD/비행시간)
 * @param turn 편도당 지상 조업시간 (시간)
 * @param prestige 브랜드 가산 — 상징적인 기재일수록 승객이 알아본다
 */
data class AircraftType(
    val id: String,
    val name: String,
    val maker: String,
    val year: Int,
    val retire: Int,
    val seats: Int,
    val range: Int,
    val speed: Double,
    val price: Double,
    val fuel: Double,
    val maint: Double,
    val crew: Double,
    val turn: Double,
    val prestige: Double,
)

enum class Trait(val label: String) {
    EXPAND("공격적 확장"),
    VALUE("저가 물량"),
    PREMIUM("고수익 프리미엄"),
    BALANCED("균형"),
}

enum class BusinessType(
    val label: String,
    val cost: Double,
    val income: Double,
    val demandBoost: Double,
    val brandBoost: Double,
    val maintDiscount: Double,
) {
    HOTEL("호텔", 48e6, 2.9e6, 0.05, 3.0, 0.0),
    DUTY_FREE("면세점", 30e6, 2.2e6, 0.02, 2.0, 0.0),
    AGENCY("여행사", 22e6, 1.2e6, 0.06, 1.0, 0.0),
    HANGAR("정비창", 65e6, 0.0, 0.0, 0.0, 0.12),
    LOUNGE("라운지", 18e6, 0.5e6, 0.03, 4.0, 0.0),
}

data class Scenario(
    val id: String,
    val name: String,
    val startYear: Int,
    val years: Int,
    val desc: String,
)

data class Difficulty(
    val id: String,
    val name: String,
    val aiSkill: Double,
    val demandBonus: Double,
    val costMul: Double,
)

// ---------------------------------------------------------------------------
// 세이브에 들어가는 가변 상태
// ---------------------------------------------------------------------------

@Serializable
data class World(
    /** 항공유 가격 (USD/L) */
    val oil: Double,
    /** 세계 경기 지수 (1.0 = 평상) */
    val economy: Double,
    val regionEconomy: Map<Region, Double>,
    /** 기준 금리 (연리) */
    val interest: Double,
    /** 항공여행 보급 지수 — 해마다 오른다 */
    val travelIndex: Double,
    val inflation: Double,
)

/** 도시에 한시적으로 걸린 수요 배율 하나 (올림픽·전염병·관광 붐 등). */
@Serializable
data class CityEffect(val mult: Double, val untilTurn: Int)

@Serializable
data class CityState(
    /** 도시 성장 누적치 */
    val dev: Double = 1.0,
    /**
     * 겹쳐 걸린 일시 효과들. 배율 하나로 합쳐 두면 만료가 서로 다를 때 짧은 쪽이
     * 긴 쪽의 만료까지 살아남는다 — 사스 폭락이 관광 붐 끝까지 따라오는 식.
     * 따로 들고 있어야 각자 제 때 꺼진다.
     */
    val effects: List<CityEffect> = emptyList(),
    /** 공항 폐쇄 (화산·전쟁) */
    val closedUntilTurn: Int = -1,
    /** 신공항·확장으로 늘어난 슬롯 */
    val extraSlots: Int = 0,
) {
    fun isClosed(turn: Int) = turn <= closedUntilTurn

    fun boostAt(turn: Int): Double = effects
        .filter { turn <= it.untilTurn }
        .fold(1.0) { acc, e -> acc * e.mult }
        .coerceIn(0.2, 3.0)

    /** 만료된 효과를 털어낸다 — 안 그러면 20년치가 그대로 쌓인다. */
    fun pruned(turn: Int): CityState =
        if (effects.none { turn > it.untilTurn }) this else copy(effects = effects.filter { turn <= it.untilTurn })
}

@Serializable
data class Business(val type: BusinessType, val city: String)

@Serializable
data class Plane(
    val id: Int,
    val typeId: String,
    val airlineId: String,
    val ageQuarters: Int,
    val routeId: Int? = null,
    /**
     * 장부가 배율 — 자산가치를 "잔존가치 × 이 값"으로 잡는다.
     *
     * 중고기는 잔존가치보다 싸게(USED_PRICE_MUL) 사는데 장부는 잔존가치 그대로 잡으면,
     * 사는 순간 차액만큼 자기자본이 공짜로 생긴다. 매물이 무한하니 그걸로 차입 한도와
     * 최종 순위를 부풀릴 수 있다. 산 값 기준을 기체에 새겨 두면 그 구멍이 막힌다.
     */
    val valueMul: Double = 1.0,
)

@Serializable
data class Order(
    val id: Int,
    val airlineId: String,
    val typeId: String,
    val count: Int,
    val deliverTurn: Int,
)

@Serializable
data class RouteResult(
    val pax: Double = 0.0,
    val seats: Double = 0.0,
    val revenue: Double = 0.0,
    val cost: Double = 0.0,
    val share: Double = 0.0,
) {
    val profit get() = revenue - cost
    val loadFactor get() = if (seats <= 0) 0.0 else (pax / seats).coerceAtMost(1.0)
}

@Serializable
data class Route(
    val id: Int,
    val airlineId: String,
    val from: String,
    val to: String,
    /** 표준 운임 대비 배율 */
    val fareMul: Double = 1.0,
    /** 주간 왕복 편수 */
    val freq: Int = 0,
    val planeIds: List<Int> = emptyList(),
    /** 노선 단위 서비스 투자 0..2 */
    val serviceExtra: Int = 0,
    val active: Boolean = true,
    val last: RouteResult? = null,
) {
    fun touches(city: String) = from == city || to == city
}

@Serializable
data class QuarterResult(
    val turn: Int,
    val revenue: Double = 0.0,
    val cargoRevenue: Double = 0.0,
    val fuelCost: Double = 0.0,
    val crewCost: Double = 0.0,
    val maintCost: Double = 0.0,
    val landingCost: Double = 0.0,
    val paxServiceCost: Double = 0.0,
    val distributionCost: Double = 0.0,
    val overhead: Double = 0.0,
    val adSpend: Double = 0.0,
    val depreciation: Double = 0.0,
    val interestCost: Double = 0.0,
    val businessIncome: Double = 0.0,
    /** 파업 합의금 등 그 분기에만 생긴 일시 비용 */
    val extraordinaryCost: Double = 0.0,
    val tax: Double = 0.0,
    val net: Double = 0.0,
    val pax: Double = 0.0,
    val rpk: Double = 0.0,
    val asks: Double = 0.0,
    val cash: Double = 0.0,
    val debt: Double = 0.0,
    val equity: Double = 0.0,
) {
    val totalRevenue get() = revenue + cargoRevenue + businessIncome
    val operatingCost
        get() = fuelCost + crewCost + maintCost + landingCost +
            paxServiceCost + distributionCost + overhead + adSpend + depreciation + extraordinaryCost
    val loadFactor get() = if (asks <= 0) 0.0 else (rpk / asks).coerceAtMost(1.0)
}

@Serializable
data class Airline(
    val id: String,
    val name: String,
    val short: String,
    val colorArgb: Long,
    val home: String,
    val isPlayer: Boolean,
    val trait: Trait,
    val cash: Double,
    val debt: Double,
    /** 발행 주식 수 */
    val shares: Double,
    val sharePrice: Double,
    /** 기내 서비스 등급 1..5 */
    val serviceLevel: Int,
    val brand: Map<Region, Double>,
    val slots: Map<String, Int>,
    val businesses: List<Business> = emptyList(),
    /** 타사 주식 보유 (항공사 id → 주식 수) */
    val holdings: Map<String, Double> = emptyMap(),
    /**
     * 이번 분기에 이미 사들인 주식 수 (항공사 id → 주식 수). 분기가 넘어갈 때 비워진다.
     * 매수 한 번마다 따로 상한을 재면 한 분기에 여러 번 눌러 상한을 우회할 수 있어서,
     * 분기 누적으로 재야 한다.
     */
    val boughtThisQuarter: Map<String, Double> = emptyMap(),
    /** 이번 분기에 이미 발행한 신주 수. 증자 한도도 분기 누적으로 재야 우회가 막힌다. */
    val issuedThisQuarter: Double = 0.0,
    /**
     * 결산에서 한 번에 털어낼 일시 비용 (파업 합의금 등). 현금에서 바로 빼면
     * 손익계산서의 순익과 실제 현금 변동이 어긋나 리포트가 앞뒤가 안 맞게 된다.
     */
    val pendingCharges: Double = 0.0,
    val adBudget: Map<Region, Double> = emptyMap(),
    val alive: Boolean = true,
    val mergedInto: String? = null,
    val bonusKey: String = "",
    val bonusLabel: String = "",
    /** 안전·신뢰도 0..1. 사고가 나면 떨어지고 서서히 회복된다 */
    val safety: Double = 1.0,
    val negativeQuarters: Int = 0,
    val results: List<QuarterResult> = emptyList(),
) {
    fun slotsAt(city: String) = slots[city] ?: 0
    fun brandIn(region: Region) = brand[region] ?: 0.0
    val lastResult get() = results.lastOrNull()
}

enum class NewsKind { WORLD, ECONOMY, RIVAL, PLAYER, ACCIDENT, MARKET, MILESTONE }

@Serializable
data class NewsItem(
    val turn: Int,
    val kind: NewsKind,
    val headline: String,
    val body: String = "",
)

@Serializable
data class Outcome(val won: Boolean, val rank: Int, val reason: String)

@Serializable
data class GameState(
    val seed: Int,
    val rngState: Int,
    val scenarioId: String,
    val difficultyId: String,
    val turn: Int,
    val startYear: Int,
    val totalTurns: Int,
    val world: World,
    val cityState: Map<String, CityState>,
    val airlines: List<Airline>,
    val playerId: String,
    val routes: List<Route>,
    val planes: List<Plane>,
    val orders: List<Order> = emptyList(),
    val nextId: Int = 1,
    val news: List<NewsItem> = emptyList(),
    val outcome: Outcome? = null,
) {
    val year: Int get() = startYear + turn / 4
    val quarter: Int get() = turn % 4 + 1
    val endYear: Int get() = startYear + (totalTurns - 1) / 4

    /**
     * 화면·세이브 문구에 쓰는 턴. 마지막 분기를 넘기면 turn 이 totalTurns 로 올라가
     * 종료 화면이 "플레이하지도 않은 다음 해 1분기"로 뜬다 — 끝난 판은 마지막으로
     * 진행한 분기를 그대로 보여준다.
     */
    val displayTurn: Int get() = if (outcome != null && turn >= totalTurns) turn - 1 else turn
    val displayYear: Int get() = startYear + displayTurn / 4
    val displayQuarter: Int get() = displayTurn % 4 + 1

    val player: Airline get() = airlines.first { it.id == playerId }
    fun airline(id: String): Airline = airlines.first { it.id == id }
    fun airlineOrNull(id: String): Airline? = airlines.firstOrNull { it.id == id }
    val livingAirlines: List<Airline> get() = airlines.filter { it.alive }

    fun routesOf(airlineId: String) = routes.filter { it.airlineId == airlineId }
    fun planesOf(airlineId: String) = planes.filter { it.airlineId == airlineId }
    fun plane(id: Int) = planes.first { it.id == id }
    fun route(id: Int) = routes.first { it.id == id }

    /** 해당 도시에서 이 항공사가 이미 쓰고 있는 슬롯 수 (주간 왕복 편수 합) */
    fun usedSlots(airlineId: String, city: String) =
        routes.filter { it.airlineId == airlineId && it.active && it.touches(city) }.sumOf { it.freq }

    fun freeSlots(airlineId: String, city: String) =
        airline(airlineId).slotsAt(city) - usedSlots(airlineId, city)

    /** 도시의 미분양 슬롯 */
    fun unsoldSlots(city: City): Int {
        val extra = cityState[city.id]?.extraSlots ?: 0
        val taken = airlines.filter { it.alive }.sumOf { it.slotsAt(city.id) }
        return (city.slots + extra - taken).coerceAtLeast(0)
    }
}
