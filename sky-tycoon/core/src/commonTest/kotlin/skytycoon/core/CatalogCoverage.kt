package skytycoon.core

import skytycoon.core.data.AircraftCatalog
import skytycoon.core.data.Scenarios
import kotlin.test.Test
import kotlin.test.assertTrue

/** 시대마다 새 기종이 들어와야 노선 전략이 흔들린다 — 신기종 공백을 지키는 못. */
class CatalogCoverage {

    @Test
    fun `시나리오 기간에 신기종 공백이 길게 생기지 않는다`() {
        for (sc in Scenarios.all) {
            val last = sc.startYear + sc.years - 1
            val intros = AircraftCatalog.all
                .map { it.year }
                .filter { it in sc.startYear..last }
                .distinct()
                .sorted()
            assertTrue(intros.isNotEmpty(), "${sc.name}: 신기종이 하나도 없다")

            // 시작 연도부터 마지막 연도까지, 신기종 없는 구간이 6년을 넘지 않아야 한다.
            var prev = sc.startYear
            for (y in intros) {
                assertTrue(y - prev <= 6, "${sc.name}: ${prev}~${y}년 사이 ${y - prev}년간 신기종이 없다")
                prev = y
            }
            assertTrue(last - prev <= 6, "${sc.name}: ${prev}년 이후 ${last}년까지 신기종이 없다")
        }
    }

    @Test
    fun `신기종은 2028년까지 이어진다`() {
        val late = AircraftCatalog.all.filter { it.year in 2018..2028 }
        assertTrue(late.isNotEmpty(), "2018년 이후 신기종이 없다")
        val newest = late.maxOf { it.year }
        assertTrue(newest >= 2028, "가장 늦은 신기종이 ${newest}년이라 2028년까지 닿지 않는다")
        // 카탈로그가 실제로 그 해에 살 수 있는 상태여야 한다.
        for (t in late) {
            assertTrue(
                AircraftCatalog.newFor(t.year).any { it.id == t.id },
                "${t.name}이 취항 연도(${t.year})에 신조기 목록에 없다",
            )
        }
    }
}
