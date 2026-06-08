package com.inspiredandroid.kai

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DarkColorScheme
import com.inspiredandroid.kai.ui.markdown.MarkdownContent
import java.util.Locale

// One-off bench: break a chat message's render into compose / measure / draw so we can see
// whether the scroll cost is CPU (compose+measure, slow on device too) or just SOFTWARE draw
// (fast on a real GPU). SOFTWARE Skia, so absolutes are inflated — the SHAPE (which phase, and
// markdown vs plain text) is the signal. Run: ./gradlew :composeApp:benchScrollRender
private val LONG_PLAIN =
    ("탑솔라 1팀은 이번 주 루프탑 안건을 검토했습니다. 핵심 지표는 RE100 달성률과 모듈 공급 일정이며 장기적으로는 거래처 다변화가 " +
        "필요합니다. 사업개발은 남도에코와 계약 초안 검토를 마쳤고 2팀은 주차장 태양광 EPC 견적 3건을 수령했습니다. 3팀은 모듈 수입 " +
        "통관이 약 이틀 지연되었습니다. 김민준 부장과 협의해 다음 주 회의를 잡았고 그 전에 BOM 정리가 선행돼야 합니다. 자금 측면에서 " +
        "단기 유동성은 충분하나 분기 말 세금 납부 일정과 겹쳐 주의가 필요합니다. 리스크는 환율 변동으로 인한 모듈 원가 상승, 인허가 " +
        "일정 불확실성, 시공 인력 수급의 계절적 변동입니다. 결론적으로 이번 주는 계약 단계 진입이 가장 중요한 마일스톤이었습니다.").repeat(2)

private val LONG_MD = """
# 주간 업무 분석

**탑솔라** 1팀은 이번 주 *루프탑* 안건을 검토했습니다. 핵심 지표는 `RE100` 달성률과 모듈 공급 일정입니다.

## 진행 상황
- 사업개발: 남도에코와 **계약 초안** 검토 완료
- 2팀: 주차장 태양광 `EPC` 견적 3건 수령
- 3팀: 모듈 수입 통관 지연(약 2일)

김민준 부장과 협의해 **다음 주** 회의를 잡았고, 그 전에 `BOM` 정리가 선행돼야 합니다. 자금 측면에서 단기 유동성은 충분하나, 분기 말 세금 납부와 겹쳐 주의가 필요합니다.

## 리스크
1. 환율 변동으로 인한 모듈 원가 상승
2. 인허가 일정 불확실성
3. 시공 인력 수급의 계절적 변동

결론적으로, 이번 주는 계약 단계 진입이 가장 중요한 마일스톤이었습니다.
""".trimIndent()

private val TABLE_MD = LONG_MD + "\n\n| 팀 | 안건 | 상태 |\n|---|---|---|\n| 1팀 | 루프탑 | 진행 |\n| 2팀 | 주차장 | 견적 |\n| 3팀 | 모듈 | 지연 |\n"

private fun renderPhases(content: @Composable () -> Unit): Triple<Long, Long, Long> {
    val t0 = System.nanoTime()
    val scene = ImageComposeScene(width = 824, height = 1700, density = Density(2f)) {
        MaterialTheme(colorScheme = DarkColorScheme) { Surface { content() } }
    }
    val t1 = System.nanoTime() // compose (setContent during construction)
    scene.render() // first render: measure + layout + draw
    val t2 = System.nanoTime()
    scene.render() // second render: draw only (nothing changed)
    val t3 = System.nanoTime()
    scene.close()
    val compose = t1 - t0
    val draw = t3 - t2
    val measure = (t2 - t1) - draw // first render minus draw ≈ measure+layout
    return Triple(compose, measure, draw)
}

private fun bench(label: String, n: Int, content: @Composable () -> Unit) {
    repeat(8) { renderPhases(content) } // warm JIT + caches
    var c = 0L
    var m = 0L
    var d = 0L
    repeat(n) {
        val (cc, mm, dd) = renderPhases(content)
        c += cc; m += mm; d += dd
    }
    println(
        String.format(
            Locale.US,
            "PHASE %-22s compose=%5.1f  measure=%5.1f  draw=%5.1f  cpu(compose+measure)=%5.1f ms",
            label, c / n / 1e6, m / n / 1e6, d / n / 1e6, (c + m) / n / 1e6,
        ),
    )
}

fun main() {
    val n = 40
    println("BENCH median-ish avg over n=$n (SOFTWARE Skia; compare SHAPE not absolutes):")
    bench("plain-Text long", n) { Text(LONG_PLAIN, modifier = Modifier.padding(16.dp)) }
    bench("markdown long", n) { MarkdownContent(LONG_MD, modifier = Modifier.padding(16.dp)) }
    bench("markdown + table", n) { MarkdownContent(TABLE_MD, modifier = Modifier.padding(16.dp)) }
    bench("markdown x8 (screenful)", n) {
        Column(Modifier.padding(16.dp)) { repeat(8) { MarkdownContent(LONG_MD) } }
    }
}
