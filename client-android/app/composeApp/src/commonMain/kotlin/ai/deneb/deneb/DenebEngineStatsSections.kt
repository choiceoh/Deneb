package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDay
import ai.deneb.deneb.generated.EngineRoutingRow
import ai.deneb.deneb.generated.EngineStatusResult
import ai.deneb.deneb.generated.EngineTotals
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/*
 * The statistics half of the engine screen: the window's totals, one
 * expandable row per day, the engine's internals and the router's per-entry
 * account. Every number is the engine's, the ledger's or the router's own —
 * the gateway's clock measures nothing here.
 */

/**
 * The whole window folded into one card. Every rate here is recomputed from the
 * summed work, not averaged across days — a quiet Sunday must not weigh as much
 * as a busy Monday.
 */
@Composable
internal fun EngineSummarySection(total: EngineTotals) {
    DenebGroup(label = "최근 ${total.days}일 합계") {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            EngineStatLine("요청", "${total.requests}회")
            EngineStatLine("토큰", "입력 ${formatTokenCount(total.promptTokens)} · 출력 ${formatTokenCount(total.generatedTokens)}")
            EngineStatLine(
                "속도",
                "디코드 ${formatRate(total.decodeTokensPerSec)} · 프리필 ${formatRate(total.prefillTokensPerSec)}" +
                    (if (total.sampleIsThin()) " (표본 부족)" else ""),
            )
            EngineStatLine(
                "첫 토큰",
                formatSeconds(total.meanTtftSeconds) +
                    (if (total.meanQueueSeconds > 0.0) " (대기 ${formatSeconds(total.meanQueueSeconds)})" else ""),
            )
            if (total.meanE2eSeconds > 0.0) EngineStatLine("응답", formatSeconds(total.meanE2eSeconds))
            EngineStatLine("토큰 재사용", engineTokenCacheText(total.promptCacheMeasured, total.promptCacheHitRatio, total.cachedPromptTokens, total.cachePromptTokens))
            if (total.prefixLookupRequests > 0) {
                EngineStatLine("캐시 적중 요청", engineRequestCacheText(total.prefixRequestHitRatio, total.prefixHitRequests, total.prefixLookupRequests))
            }
            if (total.specDraftTokens > 0) {
                EngineStatLine("드래프트 수락", "${formatPercent(total.specAcceptRatio * 100)} · ${formatTokenCount(total.specDraftTokens)} 제안")
            }
            EngineStatLine("가동", "${formatPercent(total.utilization * 100)} · ${formatDuration(total.busySeconds)}")
            if (total.outages > 0 || total.downSeconds > 0.0) {
                EngineStatLine("끊김", "${total.outages}회 · ${formatDuration(total.downSeconds)}")
            }
            if (total.routerLocalRequests + total.routerRemoteRequests + total.routerUnknownRequests > 0) {
                val pct = sharePercent(total.routerLocalRequests, total.routerRemoteRequests + total.routerUnknownRequests)
                EngineStatLine(
                    "라우터",
                    "로컬 ${formatPercent(pct ?: 0.0)} · 로컬 ${total.routerLocalRequests} · 원격 ${total.routerRemoteRequests}" +
                        (if (total.routerUnknownRequests > 0) " · 미상 ${total.routerUnknownRequests}" else ""),
                )
            }
            if (total.restarts > 0) EngineStatLine("카운터 리셋", "${total.restarts}회")
        }
    }
}

/** One label/value line in a stat card. */
@Composable
internal fun EngineStatLine(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text = label, style = DenebType.rowSubtitle, color = denebHint(), modifier = Modifier.weight(1f))
        Text(
            text = value,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/**
 * One row per day, newest first — including a day the engine was gone for
 * entirely, which has no counters but does have a downtime. A tap expands the
 * day to every figure the engine, the ledger and the router hold for it; the
 * newest day starts expanded.
 */
@Composable
internal fun EngineDaysSection(days: List<EngineDay>) {
    DenebGroup(label = "날짜별") {
        if (days.isEmpty()) {
            Text(
                text = "아직 측정된 날이 없습니다 — 표본은 엔진이 살아 있는 동안에만 쌓입니다.",
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
            )
            return@DenebGroup
        }
        // A thin day must not set the scale. Its rate swings with a handful of
        // intervals, so letting it define "fastest" would shrink every settled
        // day's bar against a number that is mostly noise.
        val fastest = days.filter { it.measured && !it.sampleIsThin() }
            .maxOfOrNull { it.decodeTokensPerSec } ?: 0.0
        var expanded by remember(days.firstOrNull()?.day) { mutableStateOf(setOfNotNull(days.firstOrNull()?.day)) }
        days.forEach { day ->
            EngineDayRow(
                day = day,
                fastestDecode = fastest,
                expanded = day.day in expanded,
                onToggle = { expanded = if (day.day in expanded) expanded - day.day else expanded + day.day },
            )
        }
        Spacer(Modifier.height(12.dp))
    }
}

@Composable
private fun EngineDayRow(day: EngineDay, fastestDecode: Double, expanded: Boolean, onToggle: () -> Unit) {
    Column(
        Modifier.fillMaxWidth()
            .denebPressable(onClick = onToggle, role = Role.Button, onClickLabel = if (expanded) "접기" else "상세")
            .padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 6.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = day.day,
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = when {
                    day.measured -> "디코드 ${formatRate(day.decodeTokensPerSec)} · 프리필 ${formatRate(day.prefillTokensPerSec)}"
                    day.downSeconds > 0.0 -> "측정 없음 · 끊김 ${formatDuration(day.downSeconds)}"
                    else -> "측정 없음"
                },
                style = DenebType.meta,
                color = denebHint(),
            )
        }
        // Decode speed against the window's fastest day, so a slow day is
        // visible without reading every number. A thin day gets no bar —
        // placing it on the scale would assert a precision it does not have.
        if (day.measured && fastestDecode > 0.0 && !day.sampleIsThin()) {
            Spacer(Modifier.height(4.dp))
            ShareBar(fraction = (day.decodeTokensPerSec / fastestDecode).toFloat())
        }
        // Collapsed: the one line that says whether the day was normal.
        val brief = buildList {
            if (day.outages > 0 || day.downSeconds > 0.0) add("끊김 ${day.outages}회 · ${formatDuration(day.downSeconds)}")
            if (day.measured) add("요청 ${day.requests}")
            if (day.routerMetered) {
                sharePercent(day.routerLocalRequests, day.routerRemoteRequests + day.routerUnknownRequests)?.let { add("로컬 ${formatPercent(it)}") }
            }
        }.joinToString(" · ")
        if (brief.isNotBlank() && !expanded) {
            Text(text = brief, style = DenebType.meta, color = denebHint(), modifier = Modifier.padding(top = 6.dp))
        }
        if (expanded) EngineDayDetail(day)
    }
}

/** Every figure the day has, as label/value lines. */
@Composable
private fun EngineDayDetail(day: EngineDay) {
    Column(Modifier.fillMaxWidth().padding(top = 6.dp)) {
        if (day.measured) {
            EngineStatLine("요청", "${day.requests}회")
            EngineStatLine("토큰", "입력 ${formatTokenCount(day.promptTokens)} · 출력 ${formatTokenCount(day.generatedTokens)}")
            EngineStatLine(
                "첫 토큰",
                formatSeconds(day.meanTtftSeconds) + (if (day.meanQueueSeconds > 0.0) " (대기 ${formatSeconds(day.meanQueueSeconds)})" else ""),
            )
            EngineStatLine("응답", formatSeconds(day.meanE2eSeconds))
            EngineStatLine("동시성", "${formatConcurrency(day.concurrencyWhileBusy)} · 최대 ${day.peakConcurrency}")
            EngineStatLine(
                "토큰 재사용",
                engineTokenCacheText(day.promptCacheMeasured, day.promptCacheHitRatio, day.cachedPromptTokens, day.cachePromptTokens),
            )
            if (day.prefixLookupRequests > 0) {
                EngineStatLine("캐시 적중 요청", engineRequestCacheText(day.prefixRequestHitRatio, day.prefixHitRequests, day.prefixLookupRequests))
            }
            if (day.specDraftTokens > 0) {
                EngineStatLine("드래프트 수락", "${formatPercent(day.specAcceptRatio * 100)} · ${formatTokenCount(day.specDraftTokens)} 제안")
            }
            EngineStatLine("가동", "${formatPercent(day.utilization * 100)} · ${formatDuration(day.busySeconds)} / ${formatDuration(day.observedSeconds)}")
            if (day.sampleIsThin()) {
                Text(
                    text = "표본 ${day.generatedTokens}토큰 — 속도는 참고만",
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
        if (day.livenessTracked) {
            EngineStatLine("끊김", if (day.outages == 0 && day.downSeconds <= 0.0) "없음" else "${day.outages}회 · ${formatDuration(day.downSeconds)}")
        }
        if (day.routerMetered) {
            val pct = sharePercent(day.routerLocalRequests, day.routerRemoteRequests + day.routerUnknownRequests)
            EngineStatLine(
                "라우터",
                (if (pct != null) "로컬 ${formatPercent(pct)} · " else "") +
                    "로컬 ${day.routerLocalRequests} · 원격 ${day.routerRemoteRequests}" +
                    (if (day.routerUnknownRequests > 0) " · 미상 ${day.routerUnknownRequests}" else ""),
            )
        }
        if (day.restarts > 0) {
            Text(
                text = "카운터 리셋 ${day.restarts}회 — 그 구간은 위 수치에 없습니다",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        if (day.pollIntervalSec > 0 && day.measured) {
            Text(
                text = "최대 동시성은 ${day.pollIntervalSec}초 폴링이 본 최댓값이라 하한입니다.",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

/**
 * What the engine says about itself right now: the prefix-cache budget that
 * decides whether a 40K head is resumed or re-prefilled, KV occupancy, GPU
 * memory, and the fleet lock — the cause behind a "connection refused".
 */
@Composable
internal fun EngineInternalsSection(status: EngineStatusResult) {
    val i = status.internals
    DenebGroup(label = "엔진 내부") {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 14.dp)) {
            if (i.published) {
                EngineStatLine(
                    "프리픽스 스냅샷",
                    "${i.prefixEntries} 사용 · 여유 ${i.prefixSnapshotsFree}" +
                        (if (i.prefixPinnedEntries > 0) " · 고정 ${i.prefixPinnedEntries}" else "") +
                        (if (i.prefixTierEntries > 0) " · 디스크 ${i.prefixTierEntries}" else ""),
                )
                EngineStatLine("KV 블록", "${i.kvBlocksUsed} / ${i.kvBlocksTotal} 사용 · 캐시 ${i.kvBlocksCached}")
                EngineStatLine("대기 대화", "${i.conversationsParked}")
                EngineStatLine(
                    "GPU 메모리",
                    "여유 ${formatGigabytes(i.deviceMemoryFreeBytes)} / ${formatGigabytes(i.deviceMemoryTotalBytes)}" +
                        (if (i.deviceMemoryReservedBytes > 0) " · 예약 ${formatGigabytes(i.deviceMemoryReservedBytes)}" else ""),
                )
                if (i.hostMemoryAvailableBytes > 0) EngineStatLine("호스트 메모리", "여유 ${formatGigabytes(i.hostMemoryAvailableBytes)}")
                if (i.handingOver || i.quiet) {
                    EngineStatLine("상태", listOfNotNull(if (i.handingOver) "핸드오버 중" else null, if (i.quiet) "유휴" else null).joinToString(" · "))
                }
                if (i.prefixSnapshotsFree == 0 && i.prefixEntries > 0) {
                    Text(
                        text = "스냅샷 여유가 0이면 메모리 캐시가 가득 찬 상태입니다. 이전 경계는 압축 캐시나 저장 장치에서 복구될 수 있습니다.",
                        style = DenebType.meta,
                        color = denebHint(),
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }
            if (i.fleetKnown) {
                EngineStatLine("플릿 소유", i.fleetOwner.ifBlank { "—" })
                if (i.fleetDraining.isNotBlank()) EngineStatLine("드레인 대상", i.fleetDraining)
                if (i.fleetHandedOver.isNotBlank()) EngineStatLine("핸드오버 완료", i.fleetHandedOver)
            }
            if (i.served > 0 || i.steps > 0) {
                EngineStatLine("이 프로세스", "요청 ${i.served} · 스텝 ${i.steps}")
            }
        }
    }
}

/** The router's per-entry account: who answered, how much, and what state the
 *  router holds the entry in right now. */
@Composable
internal fun EngineRoutingSection(label: String, rows: List<EngineRoutingRow>) {
    DenebGroup(label = label) {
        rows.forEach { row ->
            Row(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        text = row.model,
                        style = if (row.local) DenebType.rowTitleStrong else DenebType.rowTitle,
                        color = MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    val kind = when {
                        row.local -> "로컬 엔진"
                        row.known -> "원격"
                        else -> "설정에 없음"
                    }
                    Text(
                        text = kind + routingStateSuffix(row),
                        style = DenebType.meta,
                        color = if (row.local) MaterialTheme.colorScheme.primary else denebHint(),
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Text(
                    text = "${row.requests}회 · 입력 ${formatTokenCount(row.inputTokens)} · 출력 ${formatTokenCount(row.outputTokens)}",
                    style = DenebType.meta,
                    color = denebHint(),
                )
            }
        }
        Spacer(Modifier.height(12.dp))
    }
}

/** The router's live state for an entry, as a suffix on its kind label. */
internal fun routingStateSuffix(row: EngineRoutingRow): String = buildList {
    when (row.circuitState) {
        "open" -> add("서킷 열림" + (if (row.retryAfterMs > 0) " (${formatDuration(row.retryAfterMs / 1000.0)} 후 재시도)" else ""))
        "half_open" -> add("서킷 반개방")
        "degraded" -> add("서킷 저하" + (if (row.circuitFailures > 0) " (실패 ${row.circuitFailures})" else ""))
    }
    when (row.keyHealth) {
        "auth_failed" -> add("키 거부")
        "rate_limited" -> add("한도 초과")
        "unreachable" -> add("업스트림 불통")
    }
    if (row.upstreamMissing) add("업스트림에 모델 없음")
}.joinToString("") { " · $it" }
