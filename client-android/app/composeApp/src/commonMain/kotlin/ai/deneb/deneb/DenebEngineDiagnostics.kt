package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDiagnosticWindow
import ai.deneb.deneb.generated.EngineMeasure
import ai.deneb.deneb.generated.EngineStatusResult
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebSegment
import ai.deneb.ui.components.DenebSegmentedRow
import ai.deneb.ui.denebHint
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import kotlinx.datetime.TimeZone
import kotlin.math.roundToInt

/** Operational history from real interval deltas, including zero and missing values. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun EngineDiagnosticsSection(status: EngineStatusResult, zone: TimeZone) {
    val report = status.diagnostics
    var minutes by remember { mutableStateOf(30) }
    val window = report.windows.firstOrNull { it.minutes == minutes }
    DenebGroup(label = "실행 통계") {
        DiagnosticHint(
            when {
                report.lastSampleMs <= 0L -> "아직 수집된 실행 통계가 없습니다."
                report.stale -> "수집 지연 · 마지막 표본 ${formatClockOrDay(report.lastSampleMs, status.nowMs, zone)}"
                else -> "마지막 표본 ${formatClock(report.lastSampleMs, zone)} · 15초 간격 수집"
            },
        )
        DenebSegmentedRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            listOf(30 to "30분", 1440 to "24시간").forEachIndexed { index, (value, label) ->
                DenebSegment(selected = minutes == value, onClick = { minutes = value }, index = index, count = 2) {
                    Text(label)
                }
            }
        }
        if (window != null) {
            window.metrics.forEach { DiagnosticMeasure(it) }
            DiagnosticHint("현재 실행의 유효 관측 ${formatDuration(window.observedSeconds)} · 표본 없는 항목은 —")
            DiagnosticHint("호스트 회차는 그래프 호출 단위입니다. 기기에서 여러 스텝을 묶어 실행하면 완료 스텝 수와 다릅니다.")
        }
    }
    if (window == null) return
    EngineTrendSection(window, status, zone)
    EngineStageSection(window)
    DiagnosticMeasureGroup("첫 토큰·응답 분포", window.latency, "P50·P95는 히스토그램 추정값입니다. 최상위 버킷을 넘으면 —로 표시합니다.")
    DiagnosticMeasureGroup("드래프트 수용 분포", window.acceptance, "각 구간은 해당 개수의 드래프트를 수용한 디코드 구간의 비중입니다.")
    EngineConditionSection(window)
    DiagnosticMeasureGroup("추론·답변 길이", window.lengths, "완료된 채팅 선택지 기준입니다. 답변은 본문 토큰이며 도구 호출은 별도입니다.")
    DenebGroup(label = "비교 조건") {
        DiagnosticHint(runtimeDescription(window.runtime))
        DiagnosticHint("요약은 현재 실행만 집계합니다. 재시작·빌드 변경 전 기록은 추이에만 남습니다.")
    }
}

@Composable
private fun DiagnosticHint(text: String) {
    Text(text, style = DenebType.meta, color = denebHint(), modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp))
}

@Composable
private fun DiagnosticMeasure(measure: EngineMeasure) {
    Column(Modifier.fillMaxWidth()) {
        DiagnosticStatLine(measure.label, diagnosticValue(measure))
        if (measure.available) DiagnosticHint("표본 ${formatTokenCount(measure.samples.toLong())}")
    }
}

@Composable
private fun DiagnosticStatLine(label: String, value: String) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) { EngineStatLine(label, value) }
}

internal fun diagnosticValue(measure: EngineMeasure): String {
    if (!measure.available || !measure.value.isFinite()) return "—"
    val value = (measure.value * 10).roundToInt() / 10.0
    return "$value ${measure.unit}"
}

internal fun runtimeDescription(runtime: String): String {
    if (runtime.isBlank()) return "실행 식별 정보 없음"
    val fields = runtime.split('|').drop(1).mapNotNull {
        val pair = it.split('=', limit = 2)
        if (pair.size == 2) pair[0] to pair[1] else null
    }.toMap()
    return "빌드 ${fields["build"]?.take(12) ?: "미제공"} · 부팅 ${fields["boot"]?.take(8) ?: "미제공"}" +
        " · K ${fields["k"] ?: "미제공"} · 드래프트 ${fields["precision"] ?: "미제공"}"
}

@Composable
private fun DiagnosticMeasureGroup(label: String, measures: List<EngineMeasure>, note: String) {
    DenebGroup(label = label) {
        if (measures.none { it.available }) {
            DiagnosticHint("현재 실행에서 측정된 표본이 없습니다.")
        } else {
            measures.forEach { DiagnosticMeasure(it) }
        }
        DiagnosticHint(note)
    }
}

@Composable
private fun EngineStageSection(window: EngineDiagnosticWindow) {
    val names = mapOf("propose" to "드래프트", "forward" to "타깃 검증", "observe" to "상태 갱신", "sample" to "토큰 선택")
    DiagnosticMeasureGroup(
        "단계별 실행 비용",
        window.stages.map { it.copy(label = names[it.key] ?: it.label) },
        "표본 스텝의 기기 시간입니다. 겹치는 구간은 합쳐서 전체 지연으로 계산하지 않습니다.",
    )
}

@Composable
private fun EngineConditionSection(window: EngineDiagnosticWindow) {
    DenebGroup(label = "같은 조건끼리 비교") {
        if (window.conditions.isEmpty()) DiagnosticHint("조건별 계측 표본이 없습니다.")
        window.conditions.forEach { row ->
            val cache = when (row.cache) {
                "cold" -> "캐시 없음"
                "hit" -> "캐시 재사용"
                "mixed" -> "캐시 혼합"
                else -> "캐시 미상"
            }
            val timing = when (row.timing) {
                "device_burst" -> "기기 실행시간"
                "async_residency" -> "비동기 완료시간·중첩 가능"
                else -> "동기 실행시간"
            }
            DiagnosticHint("C=${row.sequences} · 최대 문맥 ${contextBandLabel(row.context)} · $cache")
            DiagnosticStatLine("속도 · 스텝당 출력", "${oneDecimal(row.stepRate)} step/s · ${oneDecimal(row.tokensPerStep)} tok")
            DiagnosticStatLine("수용률", if (row.draftTokens > 0) "${oneDecimal(row.acceptance)}%" else "—")
            DiagnosticHint("$timing · ${row.steps.toLong()}스텝")
        }
        DiagnosticHint("동시성·최대 실제 문맥·캐시·측정방식이 같은 행끼리 비교합니다. 그래프 용량은 실제 문맥 길이와 다릅니다.")
    }
}

private fun oneDecimal(value: Double): Double = (value * 10).roundToInt() / 10.0

private fun contextBandLabel(value: String): String = when (value) {
    "8192" -> "8K 이하"
    "32768" -> "8~32K"
    "131072" -> "32~128K"
    "over128k" -> "128K 초과"
    else -> value
}

@Composable
private fun EngineTrendSection(window: EngineDiagnosticWindow, status: EngineStatusResult, zone: TimeZone) {
    DenebGroup(label = "시간별 변화") {
        val points = window.points
        if (points.isEmpty()) {
            DiagnosticHint("아직 추이를 그릴 표본이 없습니다.")
            return@DenebGroup
        }
        val from = status.nowMs - window.minutes * 60_000L
        val until = status.nowMs
        TrendLine("호스트 step/s", window, status, from, until) { if (it.hasStep) it.stepRate else null }
        TrendLine("요청 tok/s", window, status, from, until) { if (it.hasDecode) it.decodeRate else null }
        TrendLine("수용률 %", window, status, from, until) { if (it.hasAcceptance) it.acceptance else null }
        DiagnosticHint("${formatClockOrDay(from, until, zone)} ~ ${formatClock(until, zone)} · 빈 구간은 미측정")
        DiagnosticHint("세로선은 실행·수집 변경, 음영은 클라우드 우회 구간입니다.")
        window.events.takeLast(12).forEach { event ->
            val label = when (event.kind) {
                "runtime_changed" -> "실행 변경"
                "counter_reset" -> "카운터 리셋"
                "sampling_gap" -> "수집 공백"
                "incomplete_sample" -> "불완전 표본 제외"
                else -> "수집 시작"
            }
            DiagnosticHint("${formatClock(event.atMs, zone)} · $label")
        }
        status.outages.filter { it.sinceMs < until && (it.untilMs == 0L || it.untilMs > from) }.takeLast(12).forEach {
            DiagnosticHint("${formatClockOrDay(it.sinceMs, until, zone)} · 클라우드 우회 · ${downReasonLabel(it.reason)}")
        }
    }
}

@Composable
private fun TrendLine(
    label: String,
    window: EngineDiagnosticWindow,
    status: EngineStatusResult,
    from: Long,
    until: Long,
    value: (ai.deneb.deneb.generated.EngineTrendPoint) -> Double?,
) {
    val values = window.points.mapNotNull(value)
    val maximum = values.maxOrNull()?.coerceAtLeast(1.0) ?: 1.0
    DiagnosticHint("$label · 최근 ${values.lastOrNull()?.let(::oneDecimal) ?: "—"}")
    val color = MaterialTheme.colorScheme.primary
    val marker = MaterialTheme.colorScheme.error
    Canvas(Modifier.fillMaxWidth().height(60.dp).padding(horizontal = 16.dp).semantics { contentDescription = "$label 시간별 변화" }) {
        fun x(at: Long): Float = ((at - from).toDouble() / (until - from).coerceAtLeast(1)).toFloat() * size.width
        status.outages.forEach { outage ->
            val start = outage.sinceMs.coerceAtLeast(from)
            val end = (outage.untilMs.takeIf { it > 0 } ?: until).coerceAtMost(until)
            if (end > start) drawRect(marker.copy(alpha = 0.12f), Offset(x(start), 0f), Size(x(end) - x(start), size.height))
        }
        window.points.forEach { p ->
            val v = value(p)
            if (v != null && v.isFinite()) {
                val y = size.height - (v / maximum).toFloat() * size.height
                drawLine(color, Offset(x(p.sinceMs), y), Offset(x(p.untilMs), y), strokeWidth = 3.dp.toPx())
            }
        }
        window.events.forEach { e -> drawLine(marker, Offset(x(e.atMs), 0f), Offset(x(e.atMs), size.height), strokeWidth = 1.dp.toPx()) }
    }
}
