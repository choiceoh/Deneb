package ai.deneb

import ai.deneb.deneb.generated.EngineCondition
import ai.deneb.deneb.generated.EngineDiagnosticEvent
import ai.deneb.deneb.generated.EngineDiagnosticWindow
import ai.deneb.deneb.generated.EngineDiagnosticsReport
import ai.deneb.deneb.generated.EngineMeasure
import ai.deneb.deneb.generated.EngineTrendPoint

/** Synthetic counters for visual coverage; never used as measured performance. */
internal val sampleEngineDiagnostics: EngineDiagnosticsReport = run {
    val runtime = "glm st:runtime_info|boot=fixture-boot|build=fixture-build|k=7|precision=w4"
    fun measure(key: String, label: String, value: Double, unit: String, samples: Double = 1000.0) = EngineMeasure(key = key, label = label, value = value, unit = unit, available = true, samples = samples)
    val window = EngineDiagnosticWindow(
        minutes = 30,
        runtime = runtime,
        observedSeconds = 900.0,
        metrics = listOf(
            measure("steps", "호스트 디코드 회차", 20.1, "step/s"),
            measure("tokens_step", "완료 스텝당 출력", 3.7, "tok/step"),
            measure("decode", "요청 디코드", 74.3, "tok/s"),
            measure("acceptance", "드래프트 수용률", 52.8, "%"),
            measure("throughput", "구간 합산 처리량", 18.4, "tok/s"),
            measure("prefill", "실제 프리필", 1870.0, "tok/s"),
        ),
        stages = listOf(
            measure("propose", "propose", 2.8, "ms/step", 256.0),
            measure("forward", "forward", 47.0, "ms/step", 256.0),
            measure("observe", "observe", 0.6, "ms/step", 256.0),
        ),
        latency = listOf(
            measure("ttft50", "첫 토큰 P50", 850.0, "ms", 240.0),
            measure("ttft95", "첫 토큰 P95", 6200.0, "ms", 240.0),
            measure("queue50", "대기 P50", 0.0, "ms", 240.0),
            measure("queue95", "대기 P95", 950.0, "ms", 240.0),
            measure("e2e50", "전체 응답 P50", 18000.0, "ms", 230.0),
            measure("e2e95", "전체 응답 P95", 72000.0, "ms", 230.0),
        ),
        acceptance = listOf(10, 7, 7, 20, 24, 15, 10, 7).mapIndexed { index, count ->
            measure("$index", "${index}개 수용", count.toDouble(), "%", count * 10.0)
        },
        lengths = listOf(
            measure("reasoning", "추론 평균 길이", 1800.0, "tok", 230.0),
            measure("reasoning50", "추론 P50", 1500.0, "tok", 230.0),
            measure("reasoning95", "추론 P95", 6400.0, "tok", 230.0),
            measure("answer", "답변 평균 길이", 240.0, "tok", 230.0),
            measure("answer50", "답변 P50", 200.0, "tok", 230.0),
            measure("answer95", "답변 P95", 800.0, "tok", 230.0),
            measure("length_limit", "토큰 제한 종료", 6.1, "%", 230.0),
        ),
        conditions = listOf(
            EngineCondition(sequences = "1", context = "8192", cache = "cold", timing = "sync_wall", steps = 1800.0, stepRate = 20.1, tokensPerStep = 3.7, acceptance = 52.8, draftTokens = 12600.0),
            EngineCondition(sequences = "4", context = "131072", cache = "mixed", timing = "device_burst", steps = 800.0, stepRate = 14.1, tokensPerStep = 12.4, acceptance = 44.3, draftTokens = 22400.0),
        ),
        points = (0..8).map { i ->
            EngineTrendPoint(
                sinceMs = PREVIEW_NOW_MS - (30 - i * 2) * 60_000L,
                untilMs = PREVIEW_NOW_MS - (29 - i * 2) * 60_000L,
                runtime = runtime,
                stepRate = 19.0 + i % 3,
                decodeRate = 67.0 + i % 4 * 3,
                acceptance = 45.0 + i % 5 * 2,
                hasStep = true, hasDecode = true, hasAcceptance = true,
            )
        },
        events = listOf(EngineDiagnosticEvent(atMs = PREVIEW_NOW_MS - 18 * 60_000L, kind = "runtime_changed")),
    )
    EngineDiagnosticsReport(lastSampleMs = PREVIEW_NOW_MS - 13 * 60_000L, stale = true, windows = listOf(window, window.copy(minutes = 1440)))
}
