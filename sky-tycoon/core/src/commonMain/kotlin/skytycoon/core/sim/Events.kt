package skytycoon.core.sim

import skytycoon.core.data.Cities
import skytycoon.core.model.CityState
import skytycoon.core.model.GameState
import skytycoon.core.model.NewsItem
import skytycoon.core.model.NewsKind
import skytycoon.core.model.Region
import kotlin.math.pow

private typealias Effect = (GameState, Rng) -> GameState

private class Scripted(
    val year: Int,
    val quarter: Int,
    val kind: NewsKind,
    val headline: String,
    val body: String,
    val effect: Effect = { s, _ -> s },
)

object Events {

    // ------------------------------------------------------------- 상태 조작 헬퍼

    private fun GameState.news(kind: NewsKind, headline: String, body: String = "") =
        copy(news = news + NewsItem(turn, kind, headline, body))

    private fun GameState.oilMul(m: Double) = copy(world = world.copy(oil = world.oil * m))

    private fun GameState.economyAdd(d: Double) =
        copy(world = world.copy(economy = (world.economy + d).coerceIn(0.45, 1.45)))

    private fun GameState.regionMul(region: Region, m: Double) = copy(
        world = world.copy(
            regionEconomy = world.regionEconomy +
                (region to ((world.regionEconomy[region] ?: 1.0) * m).coerceIn(0.4, 1.6)),
        ),
    )

    private fun GameState.interest(v: Double) = copy(world = world.copy(interest = v.coerceIn(0.02, 0.22)))

    private fun GameState.cityBoost(cityId: String, mult: Double, quarters: Int): GameState {
        val cs = cityState[cityId] ?: CityState()
        return copy(
            cityState = cityState + (cityId to cs.copy(boost = mult, boostUntilTurn = turn + quarters - 1)),
        )
    }

    private fun GameState.cityClose(cityId: String, quarters: Int): GameState {
        val cs = cityState[cityId] ?: CityState()
        return copy(cityState = cityState + (cityId to cs.copy(closedUntilTurn = turn + quarters - 1)))
    }

    private fun GameState.addSlots(cityId: String, n: Int): GameState {
        val cs = cityState[cityId] ?: CityState()
        return copy(cityState = cityState + (cityId to cs.copy(extraSlots = cs.extraSlots + n)))
    }

    private fun GameState.hitSafety(airlineId: String, delta: Double) =
        withAirline(airlineId) { it.copy(safety = (it.safety + delta).coerceIn(0.55, 1.0)) }

    // ------------------------------------------------------------- 역사 이벤트

    private val scripted: List<Scripted> = listOf(
        Scripted(1971, 3, NewsKind.ECONOMY, "닉슨 쇼크 — 금태환 정지", "브레튼우즈 체제가 무너지고 환율이 요동칩니다.") { s, _ ->
            s.copy(world = s.world.copy(inflation = s.world.inflation * 1.02)).economyAdd(-0.03)
        },
        Scripted(1973, 4, NewsKind.WORLD, "제1차 오일쇼크", "중동 산유국의 금수 조치로 항공유 가격이 폭등했습니다.") { s, _ ->
            s.oilMul(3.0).economyAdd(-0.12).regionMul(Region.ME, 1.15)
        },
        Scripted(1974, 3, NewsKind.ECONOMY, "세계 동시 불황", "각국 항공 수요가 얼어붙었습니다.") { s, _ -> s.economyAdd(-0.06) },
        Scripted(1975, 3, NewsKind.ECONOMY, "경기 회복 조짐", "여객 수요가 서서히 돌아옵니다.") { s, _ -> s.economyAdd(0.07) },
        Scripted(1976, 1, NewsKind.MILESTONE, "초음속 여객기 콩코드 취항", "런던·파리에서 대서양을 3시간에 건너는 시대가 열렸습니다."),
        Scripted(1978, 4, NewsKind.MARKET, "미국 항공 규제완화법", "노선과 운임이 자유화되면서 북미 시장에 운임 전쟁이 시작됩니다.") { s, _ ->
            s.regionMul(Region.NA, 1.10)
        },
        Scripted(1979, 2, NewsKind.WORLD, "제2차 오일쇼크", "이란 혁명으로 유가가 다시 두 배가 됐습니다.") { s, _ ->
            s.oilMul(2.2).economyAdd(-0.10)
        },
        Scripted(1980, 3, NewsKind.ECONOMY, "고금리 시대", "인플레이션을 잡기 위한 초고금리에 차입 비용이 치솟습니다.") { s, _ ->
            s.interest(0.145).economyAdd(-0.05)
        },
        Scripted(1982, 4, NewsKind.ECONOMY, "금리 인하 시작", "자금 조달 여건이 풀립니다.") { s, _ -> s.interest(0.105).economyAdd(0.06) },
        Scripted(1985, 3, NewsKind.ECONOMY, "플라자 합의", "엔화 절상으로 아시아발 해외여행이 급증합니다.") { s, _ ->
            s.regionMul(Region.AS, 1.10)
        },
        Scripted(1986, 1, NewsKind.WORLD, "유가 대폭락", "배럴당 유가가 반 토막 났습니다. 장거리 노선의 채산성이 살아납니다.") { s, _ ->
            s.oilMul(0.55).economyAdd(0.05)
        },
        Scripted(1990, 3, NewsKind.WORLD, "걸프 위기", "중동 정세 불안으로 유가가 뛰고 중동 노선이 얼어붙습니다.") { s, _ ->
            s.oilMul(1.7).regionMul(Region.ME, 0.75).economyAdd(-0.05)
        },
        Scripted(1991, 1, NewsKind.WORLD, "걸프전 발발", "일부 공항이 폐쇄되고 국제선 수요가 급감했습니다.") { s, _ ->
            s.cityClose("telaviv", 2).economyAdd(-0.06)
        },
        Scripted(1992, 1, NewsKind.MARKET, "유럽 단일시장 출범", "역내 항공 자유화로 유럽 노선이 열립니다.") { s, _ ->
            s.regionMul(Region.EU, 1.08)
        },
        Scripted(1997, 3, NewsKind.ECONOMY, "아시아 외환위기", "동아시아 통화가 무너지며 여객이 급감합니다.") { s, _ ->
            s.regionMul(Region.AS, 0.75)
        },
        Scripted(1999, 2, NewsKind.ECONOMY, "아시아 경제 회복", "외환위기의 그늘에서 벗어납니다.") { s, _ -> s.regionMul(Region.AS, 1.18) },
        Scripted(2001, 3, NewsKind.WORLD, "9·11 테러", "항공 안전 불안으로 전 세계 여객이 급감했습니다.") { s, _ ->
            s.economyAdd(-0.12).regionMul(Region.NA, 0.78)
        },
        Scripted(2003, 2, NewsKind.WORLD, "사스 확산", "아시아 노선이 직격탄을 맞았습니다.") { s, _ ->
            s.regionMul(Region.AS, 0.72).cityBoost("hongkong", 0.45, 3).cityBoost("beijing", 0.5, 3)
        },
        Scripted(2004, 2, NewsKind.ECONOMY, "아시아 노선 회복", "여행 수요가 빠르게 돌아옵니다.") { s, _ -> s.regionMul(Region.AS, 1.3) },
        Scripted(2008, 3, NewsKind.ECONOMY, "세계 금융위기", "신용이 얼어붙고 기업 출장이 급감합니다.") { s, _ ->
            s.economyAdd(-0.16).interest(0.13)
        },
        Scripted(2009, 4, NewsKind.ECONOMY, "양적 완화", "초저금리로 기재 발주가 되살아납니다.") { s, _ ->
            s.interest(0.045).economyAdd(0.08)
        },
        Scripted(2010, 2, NewsKind.WORLD, "아이슬란드 화산 폭발", "화산재로 유럽 하늘이 닫혔습니다.") { s, _ ->
            s.cityClose("london", 1).cityClose("paris", 1).cityClose("frankfurt", 1).cityClose("amsterdam", 1)
        },
        Scripted(2020, 1, NewsKind.WORLD, "세계적 감염병 대유행", "국경이 닫히고 여객이 증발했습니다.") { s, _ ->
            s.economyAdd(-0.40)
        },
        Scripted(2021, 4, NewsKind.ECONOMY, "국경 재개방", "억눌린 여행 수요가 폭발합니다.") { s, _ -> s.economyAdd(0.35) },

        // 대형 이벤트 — 개최 도시 수요가 한 해 동안 크게 뛴다
        Scripted(1972, 3, NewsKind.MILESTONE, "뮌헨 올림픽", "독일행 수요가 몰립니다.") { s, _ -> s.cityBoost("frankfurt", 1.7, 2) },
        Scripted(1976, 3, NewsKind.MILESTONE, "몬트리올 올림픽", "캐나다 노선이 붐빕니다.") { s, _ -> s.cityBoost("toronto", 1.7, 2) },
        Scripted(1980, 3, NewsKind.MILESTONE, "모스크바 올림픽", "모스크바 노선 수요가 급증합니다.") { s, _ -> s.cityBoost("moscow", 1.6, 2) },
        Scripted(1984, 3, NewsKind.MILESTONE, "로스앤젤레스 올림픽", "서해안이 들썩입니다.") { s, _ -> s.cityBoost("losangeles", 1.8, 2) },
        Scripted(1988, 3, NewsKind.MILESTONE, "서울 올림픽", "서울행 좌석을 구하기 어렵습니다.") { s, _ -> s.cityBoost("seoul", 1.9, 2) },
        Scripted(1992, 3, NewsKind.MILESTONE, "바르셀로나 올림픽", "스페인 노선이 만석입니다.") { s, _ -> s.cityBoost("madrid", 1.7, 2) },
        Scripted(2000, 3, NewsKind.MILESTONE, "시드니 올림픽", "호주행 수요가 폭증합니다.") { s, _ -> s.cityBoost("sydney", 1.9, 2) },
        Scripted(2008, 3, NewsKind.MILESTONE, "베이징 올림픽", "중국 노선이 폭발적으로 늘어납니다.") { s, _ -> s.cityBoost("beijing", 2.0, 2) },
        Scripted(2012, 3, NewsKind.MILESTONE, "런던 올림픽", "히스로가 사상 최대 혼잡을 기록합니다.") { s, _ -> s.cityBoost("london", 1.6, 2) },
        Scripted(2016, 3, NewsKind.MILESTONE, "리우 올림픽", "남미행 수요가 몰립니다.") { s, _ -> s.cityBoost("saopaulo", 1.8, 2) },
    )

    // ------------------------------------------------------------- 세계 경제 진행

    /** 매 분기 세계가 조금씩 움직인다. 평균 회귀 + 잡음. */
    fun stepWorld(state: GameState, rng: Rng): GameState {
        var s = state
        val w = s.world

        val targetOil = NewGame.oilFor(s.year)
        val oil = (w.oil + (targetOil - w.oil) * 0.10 + w.oil * rng.normal() * 0.035).coerceAtLeast(0.01)
        val economy = (w.economy + (1.0 - w.economy) * 0.10 + rng.normal() * 0.018).coerceIn(0.45, 1.45)
        val regionEconomy = w.regionEconomy.mapValues { (_, v) ->
            (v + (1.0 - v) * 0.09 + rng.normal() * 0.02).coerceIn(0.4, 1.6)
        }
        val interest = (w.interest + (Balance.BASE_INTEREST - w.interest) * 0.08 + rng.normal() * 0.004)
            .coerceIn(0.02, 0.22)

        return s.copy(
            world = w.copy(
                oil = oil,
                economy = economy,
                regionEconomy = regionEconomy,
                interest = interest,
            ),
        )
    }

    /**
     * 해가 바뀌는 순간(4분기 → 다음 해 1분기)에 물가·여행 보급·도시 성장을 한 번에 올린다.
     *
     * 분기 진행이 끝난 뒤에 부르는 이유: 플레이어가 새해 1분기 화면에서 슬롯이나 기재를
     * 살 때 이미 그 해 물가가 적용돼 있어야 한다. 1분기를 결산할 때 올리면 계획은
     * 작년 가격으로 세우고 결산만 새 가격으로 하는 셈이 된다.
     *
     * @param nextTurn 이번 진행이 끝나면 도달할 턴
     */
    fun yearTick(state: GameState, nextTurn: Int): GameState {
        if (nextTurn <= 0 || nextTurn % 4 != 0) return state
        val nextYear = state.startYear + nextTurn / 4
        val ratio = NewGame.inflationFor(nextYear) / NewGame.inflationFor(nextYear - 1)
        return state.copy(
            world = state.world.copy(
                inflation = state.world.inflation * ratio,
                travelIndex = state.world.travelIndex * Balance.TRAVEL_GROWTH_PER_YEAR,
            ),
            // 도시는 해마다 자란다. 지역 경기가 좋으면 더 빨리 큰다.
            cityState = state.cityState.mapValues { (id, cs) ->
                val city = Cities[id]
                val regional = (state.world.regionEconomy[city.region] ?: 1.0).pow(0.35)
                cs.copy(dev = cs.dev * (1.0 + (city.growth - 1.0) * regional))
            },
        )
    }

    /** 역사 이벤트 + 무작위 사건. */
    fun fire(state: GameState, rng: Rng): GameState {
        var s = state
        for (e in scripted) {
            if (e.year == s.year && e.quarter == s.quarter) {
                s = e.effect(s, rng).news(e.kind, e.headline, e.body)
            }
        }
        return random(s, rng)
    }

    private fun random(state: GameState, rng: Rng): GameState {
        var s = state

        // 항공 사고 — 노후 기재를 많이 굴릴수록 확률이 오른다.
        for (a in s.livingAirlines) {
            // 노선에 배속돼 실제로 뜨는 기재만 대상 — 세워둔 비행기가 착륙 사고를 낼 수는 없다.
            val planes = s.planesOf(a.id).filter { it.routeId != null }
            if (planes.isEmpty()) continue
            val avgAge = planes.sumOf { it.ageQuarters }.toDouble() / planes.size
            val p = 0.004 + (avgAge / 100.0) * 0.016 + (planes.size / 400.0)
            if (rng.chance(p.coerceAtMost(0.05))) {
                val plane = rng.pick(planes)
                val city = rng.pick(s.routes.filter { it.id == plane.routeId }.map { it.to } .ifEmpty { listOf(a.home) })
                s = s.hitSafety(a.id, -rng.range(0.05, 0.13))
                    .news(
                        NewsKind.ACCIDENT,
                        "${a.name} 여객기 사고",
                        "${Cities.name(city)} 착륙 중 사고가 발생했습니다. 신뢰도가 떨어지고 예약이 줄어듭니다.",
                    )
            }
        }

        // 승무원 파업
        if (rng.chance(0.045)) {
            val a = rng.pick(s.livingAirlines)
            val cost = 6e6 * s.world.inflation
            // 현금을 직접 빼지 않고 결산에 넘긴다 — 그래야 순익과 현금 변동이 맞는다.
            s = s.withAirline(a.id) {
                it.copy(
                    pendingCharges = it.pendingCharges + cost,
                    safety = (it.safety - 0.02).coerceAtLeast(0.55),
                )
            }
                .news(NewsKind.RIVAL, "${a.name} 승무원 파업", "임금 협상 결렬로 운항에 차질이 빚어졌습니다.")
        }

        // 공항 확장
        if (rng.chance(0.05)) {
            val city = rng.pick(Cities.all)
            val n = rng.int(6, 14)
            s = s.addSlots(city.id, n)
                .news(NewsKind.MARKET, "${city.name} 공항 확장", "슬롯 ${n}개가 새로 풀렸습니다.")
        }

        // 관광 붐
        if (rng.chance(0.08)) {
            val city = rng.pick(Cities.all.filter { it.tour >= 55 })
            s = s.cityBoost(city.id, rng.range(1.25, 1.6), rng.int(2, 4))
                .news(NewsKind.MARKET, "${city.name} 관광 붐", "여행 잡지가 앞다퉈 소개하면서 방문객이 몰립니다.")
        }

        // 자연재해로 인한 공항 폐쇄
        if (rng.chance(0.028)) {
            val city = rng.pick(Cities.all)
            s = s.cityClose(city.id, 1)
                .news(NewsKind.WORLD, "${city.name} 공항 폐쇄", "기상 재해로 이번 분기 운항이 중단됩니다.")
        }

        // 유가 급변
        if (rng.chance(0.09)) {
            val up = rng.chance(0.5)
            val m = if (up) rng.range(1.10, 1.28) else rng.range(0.78, 0.92)
            s = s.oilMul(m).news(
                NewsKind.WORLD,
                if (up) "항공유 가격 급등" else "항공유 가격 급락",
                "이번 분기 연료비가 크게 ${if (up) "올랐" else "내렸"}습니다.",
            )
        }

        // 지역 경기 서프라이즈
        if (rng.chance(0.07)) {
            val region = rng.pick(Region.entries.toList())
            val up = rng.chance(0.55)
            s = s.regionMul(region, if (up) rng.range(1.06, 1.14) else rng.range(0.86, 0.95))
                .news(
                    NewsKind.ECONOMY,
                    "${region.label} 경기 ${if (up) "호조" else "둔화"}",
                    "역내 항공 수요가 ${if (up) "늘어납니다" else "줄어듭니다"}.",
                )
        }

        return s
    }
}
