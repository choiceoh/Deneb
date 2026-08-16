package skytycoon.core.sim

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Cities
import skytycoon.core.data.Companies
import skytycoon.core.data.CompanySeed
import skytycoon.core.data.Scenarios
import skytycoon.core.model.Airline
import skytycoon.core.model.AircraftType
import skytycoon.core.model.CityState
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsItem
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Plane
import skytycoon.core.model.Region
import skytycoon.core.model.Route
import skytycoon.core.model.World
import kotlin.math.abs
import kotlin.math.pow

object NewGame {
    /** 1970년을 1.0 으로 두는 물가 지수. 원가·운임 상수가 1970년 명목가라 여기에 맞춘다. */
    fun inflationFor(year: Int): Double {
        var v = 1.0
        for (y in 1970 until year) {
            v *= when {
                y < 1980 -> 1.070
                y < 1990 -> 1.045
                y < 2000 -> 1.028
                y < 2010 -> 1.025
                else -> 1.020
            }
        }
        return v
    }

    /** 항공유 가격 (USD/L, 명목). */
    fun oilFor(year: Int): Double = when {
        year < 1974 -> 0.030
        year < 1979 -> 0.092
        year < 1986 -> 0.260
        year < 1990 -> 0.155
        year < 2000 -> 0.210
        year < 2008 -> 0.480
        else -> 0.800
    }

    fun create(
        scenarioId: String = "goldenage",
        companyId: String = "hanseong",
        difficultyId: String = "normal",
        seed: Int = 20260816,
    ): GameState {
        val scenario = Scenarios[scenarioId]
        val rng = Rng(seed)
        val year = scenario.startYear
        val inflation = inflationFor(year)

        val world = World(
            oil = oilFor(year),
            economy = 1.0,
            regionEconomy = Region.entries.associateWith { 1.0 },
            interest = Balance.BASE_INTEREST,
            travelIndex = Balance.TRAVEL_GROWTH_PER_YEAR.pow(year - 1970),
            inflation = inflation,
        )

        // 시나리오 시작 시점까지 도시들이 자라 있다.
        val cityState = Cities.all.associate { city ->
            city.id to CityState(dev = city.growth.pow(year - 1970))
        }

        var nextId = 1
        val planes = mutableListOf<Plane>()
        val airlines = mutableListOf<Airline>()

        for (seedCo in Companies.all) {
            val fleet = mutableListOf<Plane>()
            for ((typeId, count) in seedCo.startFleet) {
                val type = eraEquivalent(typeId, year)
                // 취항 전에 만들어진 기체는 없다 — 1970년에 1968년 취항 737 을
                // 6년 반 된 것으로 깔면 정비비는 부풀고 자산가치는 깎인다.
                val oldest = ((year - type.year) * 4).coerceIn(2, 26)
                repeat(count) {
                    fleet += Plane(
                        id = nextId++,
                        typeId = type.id,
                        airlineId = seedCo.id,
                        ageQuarters = rng.int(2, oldest),
                    )
                }
            }
            planes += fleet

            val isPlayer = seedCo.id == companyId
            val homeRegion = Cities[seedCo.home].region
            val brand = Region.entries.associateWith { r ->
                if (r == homeRegion) 30.0 else 8.0
            }
            airlines += Airline(
                id = seedCo.id,
                name = seedCo.name,
                short = seedCo.short,
                colorArgb = seedCo.colorArgb,
                home = seedCo.home,
                isPlayer = isPlayer,
                trait = seedCo.trait,
                cash = seedCo.cash * inflation,
                debt = 0.0,
                shares = seedCo.shares,
                sharePrice = 0.0,
                serviceLevel = if (seedCo.bonusKey == "service") 4 else 3,
                brand = brand,
                slots = seedCo.startSlots,
                bonusKey = seedCo.bonusKey,
                bonusLabel = seedCo.bonusLabel,
            )
        }

        var state = GameState(
            seed = seed,
            rngState = rng.state,
            scenarioId = scenarioId,
            difficultyId = difficultyId,
            turn = 0,
            startYear = year,
            totalTurns = scenario.years * 4,
            world = world,
            cityState = cityState,
            airlines = airlines,
            playerId = companyId,
            routes = emptyList(),
            planes = planes,
            nextId = nextId,
            news = listOf(
                NewsItem(
                    turn = 0,
                    kind = NewsKind.MILESTONE,
                    headline = "${year}년 ${scenario.name} — 취항을 개시합니다",
                    body = scenario.desc,
                ),
            ),
        )

        state = bootstrapRoutes(state, rng)
        state = Stock.repriceAll(state)
        return state.copy(rngState = rng.state)
    }

    /**
     * 창업 노선망. 각 항공사가 슬롯을 가진 도시로 모기지에서 노선을 편다.
     * 시작하자마자 굴러가는 회사를 물려받아야 첫 턴부터 판단할 거리가 생긴다.
     */
    private fun bootstrapRoutes(start: GameState, rng: Rng): GameState {
        var state = start
        val routes = mutableListOf<Route>()
        var nextId = state.nextId
        val assigned = mutableMapOf<Int, Int>() // planeId → routeId

        for (airline in state.airlines) {
            val home = airline.home
            val pool = state.planesOf(airline.id).toMutableList()
            val targets = airline.slots.keys
                .filter { it != home }
                .sortedBy { Geo.distance(home, it) }

            for (dest in targets) {
                if (pool.isEmpty()) break
                val dist = Geo.distance(home, dest)
                val planeIdx = pool.indexOfFirst { AircraftCatalog[it.typeId].range >= dist * 1.05 }
                if (planeIdx < 0) continue
                val plane = pool.removeAt(planeIdx)

                val cap = Economics.capacity(listOf(plane), dist)
                if (!cap.usable) continue
                val usedHome = routes.filter { it.airlineId == airline.id && it.touches(home) }.sumOf { it.freq }
                val usedDest = routes.filter { it.airlineId == airline.id && it.touches(dest) }.sumOf { it.freq }
                val freq = minOf(
                    cap.maxFreq,
                    airline.slotsAt(home) - usedHome,
                    airline.slotsAt(dest) - usedDest,
                    10,
                )
                if (freq <= 0) {
                    pool.add(planeIdx, plane)
                    continue
                }
                val id = nextId++
                assigned[plane.id] = id
                routes += Route(
                    id = id,
                    airlineId = airline.id,
                    from = home,
                    to = dest,
                    fareMul = 1.0 + rng.range(-0.05, 0.05),
                    freq = freq,
                    planeIds = listOf(plane.id),
                )
            }
        }

        val planes = state.planes.map { p -> assigned[p.id]?.let { p.copy(routeId = it) } ?: p }
        state = state.copy(routes = routes, planes = planes, nextId = nextId)
        return state
    }

    /**
     * 시나리오 연도에 존재하지 않는 기종을 좌석 수가 가장 비슷한 동시대 기종으로 바꾼다.
     * (1990년에 창업하는데 DC-9 신조기를 받을 수는 없다)
     */
    private fun eraEquivalent(typeId: String, year: Int): AircraftType {
        val wanted = AircraftCatalog[typeId]
        if (year >= wanted.year && year <= wanted.retire) return wanted
        val available = AircraftCatalog.newFor(year)
        if (available.isEmpty()) return wanted
        return available.minByOrNull { abs(it.seats - wanted.seats) } ?: wanted
    }
}
