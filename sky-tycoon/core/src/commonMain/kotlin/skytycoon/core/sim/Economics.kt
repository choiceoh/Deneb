package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.data.Difficulties
import skytycoon.core.model.Airline
import skytycoon.core.model.AircraftType
import skytycoon.core.model.Business
import skytycoon.core.model.BusinessType
import skytycoon.core.model.GameState
import skytycoon.core.model.Plane
import skytycoon.core.model.Route
import kotlin.math.floor
import kotlin.math.pow

/** 노선에 투입된 기재가 만들어내는 수송력. */
data class Capacity(
    val maxFreq: Int,
    val avgSeats: Double,
    val minRange: Int,
    val avgAgeQuarters: Double,
) {
    val usable: Boolean get() = maxFreq > 0
}

/** 분기 노선 원가 내역. */
data class RouteCost(
    val fuel: Double = 0.0,
    val crew: Double = 0.0,
    val maint: Double = 0.0,
    val landing: Double = 0.0,
    val nav: Double = 0.0,
    val paxService: Double = 0.0,
    val distribution: Double = 0.0,
) {
    val total: Double get() = fuel + crew + maint + landing + nav + paxService + distribution

    operator fun plus(o: RouteCost) = RouteCost(
        fuel + o.fuel, crew + o.crew, maint + o.maint, landing + o.landing,
        nav + o.nav, paxService + o.paxService, distribution + o.distribution,
    )
}

object Economics {
    /** 표준 편도 이코노미 운임 (해당 시점 명목 달러). */
    fun standardFare(distanceKm: Double, inflation: Double): Double =
        (Balance.FARE_BASE + Balance.FARE_PER_KM * distanceKm.pow(Balance.FARE_DIST_EXP)) * inflation

    /** 왕복 1회에 기재가 묶이는 시간 — 비행시간 + 양끝 지상 조업. 편수 상한을 정한다. */
    fun roundTripHours(type: AircraftType, distanceKm: Double): Double =
        2.0 * (distanceKm / type.speed + type.turn + Balance.TURNAROUND_EXTRA)

    /** 편도 블록타임 — 승무원비·정비비가 이 시간에 붙는다. */
    fun blockHoursPerLeg(type: AircraftType, distanceKm: Double): Double =
        distanceKm / type.speed + Balance.BLOCK_TAXI

    /** 이 기종으로 이 거리를 날 수 있는가 (항속거리 + 여유 5%). */
    fun canFly(type: AircraftType, distanceKm: Double): Boolean = type.range >= distanceKm * 1.05

    fun capacity(planes: List<Plane>, distanceKm: Double): Capacity {
        if (planes.isEmpty()) return Capacity(0, 0.0, 0, 0.0)
        var freqSum = 0.0
        var seatWeighted = 0.0
        var minRange = Int.MAX_VALUE
        var ageSum = 0.0
        for (p in planes) {
            val t = AircraftCatalog[p.typeId]
            val perPlane = Balance.MAX_WEEKLY_HOURS / roundTripHours(t, distanceKm)
            freqSum += perPlane
            seatWeighted += perPlane * t.seats
            if (t.range < minRange) minRange = t.range
            ageSum += p.ageQuarters
        }
        return Capacity(
            maxFreq = floor(freqSum).toInt(),
            avgSeats = if (freqSum <= 0) 0.0 else seatWeighted / freqSum,
            minRange = if (minRange == Int.MAX_VALUE) 0 else minRange,
            avgAgeQuarters = ageSum / planes.size,
        )
    }

    /** 분기 총 공급 좌석 (편도 편수 × 좌석). */
    fun quarterlySeats(freq: Int, avgSeats: Double): Double =
        freq * 2.0 * Balance.WEEKS_PER_QUARTER * avgSeats

    /** 분기 총 편도 운항 횟수. */
    fun quarterlyLegs(freq: Int): Double = freq * 2.0 * Balance.WEEKS_PER_QUARTER

    /**
     * 노선 하나의 분기 운항 원가.
     * @param pax 실제 수송 승객 수 (기내 서비스비가 여기 붙는다)
     * @param revenue 여객+화물 매출 (판매·유통비가 여기 붙는다)
     */
    fun routeCost(
        state: GameState,
        airline: Airline,
        route: Route,
        planes: List<Plane>,
        pax: Double,
        revenue: Double,
    ): RouteCost {
        if (planes.isEmpty() || route.freq <= 0) return RouteCost()
        val from = Cities[route.from]
        val to = Cities[route.to]
        val dist = Geo.distance(route.from, route.to)
        val legs = quarterlyLegs(route.freq)
        val inflation = state.world.inflation
        val costMul = Difficulties[state.difficultyId].costMul

        // 편수를 기재들이 나눠 가지므로 기종별 몫만큼 원가를 안분한다.
        var fuel = 0.0
        var crew = 0.0
        var maint = 0.0
        var landingSeats = 0.0
        var shareSum = 0.0
        val shares = planes.map { Balance.MAX_WEEKLY_HOURS / roundTripHours(AircraftCatalog[it.typeId], dist) }
        shareSum = shares.sum()
        if (shareSum <= 0.0) return RouteCost()

        val hangarDiscount = if (airline.businesses.any { it.type == BusinessType.HANGAR }) {
            1.0 - BusinessType.HANGAR.maintDiscount
        } else {
            1.0
        }
        val fuelBonus = if (airline.bonusKey == "fuel") 0.94 else 1.0

        for ((i, p) in planes.withIndex()) {
            val t = AircraftCatalog[p.typeId]
            val portion = shares[i] / shareSum
            val myLegs = legs * portion
            val block = blockHoursPerLeg(t, dist) * myLegs
            fuel += myLegs * dist * t.fuel * state.world.oil * fuelBonus
            crew += block * t.crew * inflation
            maint += block * t.maint * inflation *
                (1.0 + p.ageQuarters * Balance.AGE_MAINT_PER_QUARTER) * hangarDiscount
            landingSeats += myLegs * t.seats
        }

        val feeAvg = (from.fee + to.fee) / 2.0
        val landing = Balance.LANDING_BASE * feeAvg * (landingSeats / 150.0) * inflation
        val nav = legs * dist * Balance.NAV_PER_KM * inflation
        val serviceTotal = airline.serviceLevel + route.serviceExtra
        val paxService = pax * (Balance.PAX_SERVICE_BASE + Balance.PAX_SERVICE_PER_LEVEL * serviceTotal) * inflation
        val distribution = revenue * Balance.DISTRIBUTION_RATE

        return RouteCost(
            fuel = fuel * costMul,
            crew = crew * costMul,
            maint = maint * costMul,
            landing = landing * costMul,
            nav = nav * costMul,
            paxService = paxService * costMul,
            distribution = distribution,
        )
    }

    /**
     * 분기 감가상각 — 내용연수(15년)까지만 상각한다.
     * 기령을 안 보면 다 상각된 기재가 영원히 비용을 만들어 순익과 주가를 눌러 앉힌다.
     */
    fun depreciation(planes: List<Plane>): Double = planes
        .filter { it.ageQuarters < Balance.DEPRECIATION_QUARTERS }
        .sumOf { AircraftCatalog[it.typeId].price / Balance.DEPRECIATION_QUARTERS }

    /** 회사 유지 간접비. */
    fun overhead(state: GameState, airline: Airline): Double {
        val fleet = state.planesOf(airline.id).size
        val routes = state.routesOf(airline.id).count { it.active }
        val base = Balance.OVERHEAD_FIXED +
            fleet * Balance.OVERHEAD_PER_AIRCRAFT +
            routes * Balance.OVERHEAD_PER_ROUTE
        val serviceOpex = fleet * airline.serviceLevel * Balance.SERVICE_OPEX_PER_LEVEL_PER_PLANE
        return (base + serviceOpex) * state.world.inflation * Difficulties[state.difficultyId].costMul
    }

    /** 보유 기재의 시장가 합 (잔존가치 기준). */
    fun fleetValue(planes: List<Plane>): Double = planes.sumOf {
        AircraftCatalog[it.typeId].price * AircraftCatalog.residualRatio(it.ageQuarters) * it.valueMul
    }

    /** 슬롯 1개 매입가 — 남은 슬롯이 적을수록 급등한다. */
    fun slotPrice(state: GameState, airlineId: String, cityId: String): Double {
        val city = Cities[cityId]
        val extra = state.cityState[cityId]?.extraSlots ?: 0
        val total = (city.slots + extra).toDouble()
        val free = state.unsoldSlots(city).toDouble()
        val scarcity = ((total - free + 1.0) / total).coerceIn(0.0, 1.0)
        val airline = state.airlineOrNull(airlineId)
        val homeDiscount = if (airline?.home == cityId) Balance.SLOT_HOME_DISCOUNT else 1.0
        val negotiator = if (airline?.bonusKey == "slot") 0.8 else 1.0
        val sizeFactor = (city.econ + city.tour) / 100.0
        return Balance.SLOT_BASE_PRICE *
            sizeFactor *
            (1.0 + scarcity.pow(Balance.SLOT_SCARCITY_EXP) * 6.0) *
            city.fee *
            homeDiscount *
            negotiator *
            state.world.inflation
    }

    /**
     * 부대사업의 장부가. 매입가는 물가가 붙는데 자산가치만 명목 원가로 잡으면,
     * 후기 시나리오에서 사업을 낼 때마다 기업가치가 깎여 나간다.
     * 합병 때 겹쳐서 처분하는 시설의 매각대도 같은 기준을 쓴다.
     */
    fun businessValue(state: GameState, businesses: List<Business>): Double =
        businesses.sumOf { it.type.cost * 0.7 } * state.world.inflation

    /** 자기자본 = 현금 + 기재가치 + 슬롯가치 + 부대사업 − 부채. */
    fun equity(state: GameState, airline: Airline): Double {
        val fleet = fleetValue(state.planesOf(airline.id))
        val slots = airline.slots.entries.sumOf { (city, n) ->
            n * Balance.SLOT_BASE_PRICE * ((Cities[city].econ + Cities[city].tour) / 100.0) * state.world.inflation * 0.6
        }
        val biz = businessValue(state, airline.businesses)
        val stocks = airline.holdings.entries.sumOf { (id, shares) ->
            (state.airlineOrNull(id)?.sharePrice ?: 0.0) * shares
        }
        // 발주 대금은 이미 현금에서 빠져나갔다. 인도 전까지 선급금으로 잡아 주지 않으면
        // 대형 발주 한 번에 자기자본이 무너져 차입 한도가 깎이고 자본잠식으로 오인된다.
        val prepaid = state.orders
            .filter { it.airlineId == airline.id }
            .sumOf { AircraftCatalog[it.typeId].price * it.count }
        return airline.cash + fleet + slots + biz + stocks + prepaid - airline.debt
    }
}
