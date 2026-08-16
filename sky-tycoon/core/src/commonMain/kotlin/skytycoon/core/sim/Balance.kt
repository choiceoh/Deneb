package skytycoon.core.sim

/**
 * 게임 감각을 좌우하는 상수를 한곳에 모았다.
 *
 * 화폐 단위 주의: 모든 운임·원가 상수는 **시나리오 시작 시점의 명목 달러**로 잡혀 있고,
 * [skytycoon.core.model.World.inflation] 이 해마다 이들을 함께 밀어 올린다.
 * 기재 카탈로그 가격은 이미 시대별 명목가(747-100 = 4,500만 달러)라 물가를 따로 곱하지 않는다.
 *
 * BalanceTest 가 기준 노선(독점/경쟁)의 수지가 상식적인 범위에 있는지 지켜본다.
 */
object Balance {
    // --- 수요 ---
    /**
     * 연간 도시쌍 수요 스케일.
     *
     * 실제 서울–도쿄는 연 수백만 명이 오가지만, 그 규모를 그대로 쓰면 여객기 한 대(주 14왕복 =
     * 분기 5만 4천 석)가 시장에 아무 흔적도 남기지 못해 운임도 경쟁도 무의미해진다.
     * 그래서 수요를 게임 스케일로 압축했다 — 간선 노선 하나가 항공사 두세 곳을
     * 먹여 살리는 정도. 승객 1명은 현실의 여러 명에 해당하는 추상 단위로 읽으면 된다.
     */
    const val DEMAND_K = 3_300.0
    const val DEMAND_BIZ_EXP = 0.55
    const val DEMAND_LEISURE_W = 1.2
    const val DEMAND_DIST_HALF = 1_800.0
    const val DEMAND_DIST_EXP = 0.9
    const val DEMAND_SAME_REGION = 1.18
    /** 이보다 가까우면 육상교통에 밀린다 */
    const val DEMAND_RAIL_RANGE = 800.0
    const val DEMAND_RAIL_EXP = 0.5
    /** 항공여행 보급 지수 연간 성장 (도시 성장과 별개의 저변 확대) */
    const val TRAVEL_GROWTH_PER_YEAR = 1.02

    val SEASON_LEISURE = doubleArrayOf(0.86, 1.02, 1.32, 0.80)
    val SEASON_BIZ = doubleArrayOf(1.02, 1.06, 0.86, 1.06)

    // --- 시장 점유 (세그먼트별 다항 로짓) ---
    // 비즈니스 승객은 운임에 둔감하고 편수·서비스를 본다. 레저 승객은 그 반대.
    const val BIZ_PRICE_SENS = 1.70
    const val BIZ_FREQ_W = 0.85
    const val BIZ_SERVICE_W = 0.38
    const val LEI_PRICE_SENS = 4.20
    const val LEI_FREQ_W = 0.42
    const val LEI_SERVICE_W = 0.16

    const val SHARE_BRAND_W = 0.012
    const val SHARE_PRESTIGE_W = 0.020
    const val SHARE_HUB_W = 0.18
    const val SHARE_SAFETY_W = 0.90
    /** 좌석이 모자라 흘러넘친 수요를 재배분하는 횟수 */
    const val SPILL_ROUNDS = 3
    /** 시장 평균 운임이 표준보다 싸면 수요 자체가 늘어난다 (유발 수요) */
    const val INDUCED_ELASTICITY = 0.45

    // --- 운임 (1970년 명목 달러, 편도 이코노미) ---
    const val FARE_BASE = 22.0
    const val FARE_PER_KM = 0.030
    const val FARE_DIST_EXP = 0.97
    /** 비즈니스/퍼스트가 섞여 생기는 수익 가산 */
    const val BIZ_YIELD = 1.90
    const val LEI_YIELD = 0.95
    const val FARE_MIN_MUL = 0.55
    const val FARE_MAX_MUL = 1.80

    // --- 운항 원가 ---
    const val WEEKS_PER_QUARTER = 13.0
    /** 기재 1대의 주간 가동 상한 (시간) */
    const val MAX_WEEKLY_HOURS = 98.0
    /** 카탈로그 지상 조업시간에 더해지는 여유 (편도당) */
    const val TURNAROUND_EXTRA = 0.35
    /** 지상 활주 등으로 블록타임에 더해지는 시간 (편도당) */
    const val BLOCK_TAXI = 0.25
    /** 150석 기준 표준 착륙료 */
    const val LANDING_BASE = 520.0
    const val NAV_PER_KM = 0.030
    const val PAX_SERVICE_BASE = 5.5
    const val PAX_SERVICE_PER_LEVEL = 2.2
    const val DISTRIBUTION_RATE = 0.13
    const val OVERHEAD_PER_AIRCRAFT = 120_000.0
    const val OVERHEAD_PER_ROUTE = 18_000.0
    const val OVERHEAD_FIXED = 1.6e6
    const val DEPRECIATION_QUARTERS = 60.0
    const val CARGO_RATE = 0.13
    const val TAX_RATE = 0.30
    /** 기령 1분기마다 정비비가 이만큼씩 붙는다 */
    const val AGE_MAINT_PER_QUARTER = 0.012

    // --- 기재 거래 ---
    const val USED_PRICE_MUL = 0.72
    const val SELL_PENALTY = 0.86
    const val ORDER_DELAY_QUARTERS = 2

    // --- 슬롯 ---
    const val SLOT_BASE_PRICE = 0.42e6
    const val SLOT_SCARCITY_EXP = 1.9
    const val SLOT_HOME_DISCOUNT = 0.6

    // --- 재무 ---
    const val BASE_INTEREST = 0.085
    const val DEBT_CAP_EQUITY = 2.2
    const val BANKRUPTCY_GRACE = 4
    const val SHARE_PE = 11.0
    const val SHARE_ASSET_W = 0.45
    const val TAKEOVER_THRESHOLD = 0.5
    const val TENDER_PREMIUM = 1.3
    /** 지분이 쌓일수록 남은 주식을 사기가 비싸진다 (매집이 알려지면 값이 뛴다). */
    const val TENDER_ESCALATION = 0.9
    /** 한 분기에 사들일 수 있는 지분 상한 — 하룻밤에 회사가 넘어가지 않도록. */
    const val MAX_STAKE_PER_QUARTER = 0.10
    /** 유상증자 발행가 할인율 */
    const val ISSUE_DISCOUNT = 0.92

    // --- 마케팅 ---
    /** 광고비 100만 달러당 브랜드 포인트 */
    const val AD_EFFICIENCY = 42.0
    const val BRAND_DECAY = 0.90
    const val BRAND_MAX = 100.0
    const val SERVICE_UPGRADE_COST = 26e6
    const val SERVICE_OPEX_PER_LEVEL_PER_PLANE = 32_000.0

    // --- AI ---
    const val AI_CASH_FLOOR = 40e6
    const val AI_MAX_NEW_ROUTES = 3
    const val AI_MAX_ORDERS = 3
}
