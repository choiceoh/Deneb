package ai.deneb.deneb

import ai.deneb.deneb.generated.UsageStat
import ai.deneb.deneb.generated.UsageStatsResult
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch

/**
 * 사용량 — token usage over the last 1 or 7 days (`miniapp.usage.stats`), broken
 * down three ways: by the model that answered, by the role the run requested
 * (main/submain/coding/…), and by work-type (heartbeat, phone-event, chat, …).
 * Each row shows input tokens with a proportional bar so where the agent spends
 * its budget is visible at a glance.
 *
 * Design split (docs/agent-rules/native-design-system.md): the 1일/7일 selector
 * is a Material SingleChoiceSegmentedButton and pull-to-refresh is Material; the
 * frame + type are the Deneb skin (DenebScreenScaffold + DenebType + grouped
 * DenebGroup cards). The breakdown list is a stateless body ([UsageStatsContent])
 * the render harness previews with mock data; this composable is the stateful
 * shell (window state + fetch + loading/error/empty states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebUsageScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var days by remember { mutableStateOf(1) }
    var stats by remember { mutableStateOf<UsageStatsResult?>(null) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebRsiScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched = client.fetchUsageStats(days)
        if (fetched == null) {
            loadOk = false
        } else {
            stats = fetched
            loadOk = true
        }
    }
    // Re-fetch when the window changes; the segmented control drives days.
    LaunchedEffect(days) {
        loadOk = null
        load()
    }

    DenebScreenScaffold(title = "사용량", onBack = onBack, tabBar = navigationTabBar) {
        UsageWindowRow(days) { d ->
            if (days != d) {
                haptics.tap()
                days = d
            }
        }
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                haptics.refresh()
                scope.launch {
                    refreshing = true
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                val s = stats
                when {
                    loadOk == null && s == null -> DenebLoading()

                    loadOk == false && s == null -> DenebError(
                        "사용량을 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    s == null || (s.byModel.isEmpty() && s.byRole.isEmpty() && s.byWorkType.isEmpty()) ->
                        DenebEmpty("이 기간에 기록된 사용량이 없습니다.")

                    else -> UsageStatsContent(s)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

/** The 1일 / 7일 window selector — Material segmented control, Deneb labels. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun UsageWindowRow(days: Int, onDaysChange: (Int) -> Unit) {
    val options = listOf(1 to "1일", 7 to "7일")
    SingleChoiceSegmentedButtonRow(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 4.dp),
    ) {
        options.forEachIndexed { i, (d, label) ->
            SegmentedButton(
                selected = days == d,
                onClick = { onDaysChange(d) },
                shape = SegmentedButtonDefaults.itemShape(i, options.size),
            ) { Text(label, style = DenebType.rowSubtitle) }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The three breakdown sections (모델별 · 역할별 · 작업별) under a total line. Pure
 * presentation — the shell owns window state + fetch. Each section is a grouped
 * card of rows sorted by tokens; roles and work-types are localized to Korean.
 */
@Composable
internal fun UsageStatsContent(stats: UsageStatsResult) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        Text(
            text = "최근 ${stats.days}일 · 입력 ${formatTokenCount(stats.totalInputTokens)} · 출력 ${formatTokenCount(stats.totalOutputTokens)}",
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 12.dp),
        )
        UsageSection("모델별", stats.byModel) { it }
        Spacer(Modifier.height(18.dp))
        UsageSection("역할별", stats.byRole, ::usageRoleLabel)
        Spacer(Modifier.height(18.dp))
        UsageSection("작업별", stats.byWorkType, ::usageWorkTypeLabel)
    }
}

/** One breakdown section: a grouped card with a section label and one bar row
 *  per entry, scaled against the section's largest input-token count. Renders
 *  nothing when the list is empty (an all-zero section would just be noise). */
@Composable
private fun UsageSection(title: String, rows: List<UsageStat>, labelOf: (String) -> String) {
    if (rows.isEmpty()) return
    val maxInput = rows.maxOf { it.inputTokens }.coerceAtLeast(1)
    DenebGroup {
        Text(
            text = title,
            style = DenebType.sectionLabel,
            color = denebHint(),
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 4.dp),
        )
        rows.forEach { UsageRow(labelOf(it.name), it, maxInput) }
        Spacer(Modifier.height(12.dp))
    }
}

/** One usage row: label + input tokens, a proportional bar (input vs the
 *  section max), and a muted sub-line (runs · output · cache). */
@Composable
private fun UsageRow(label: String, stat: UsageStat, maxInput: Long) {
    val fraction = (stat.inputTokens.toFloat() / maxInput.toFloat()).coerceIn(0f, 1f)
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 8.dp, bottom = 2.dp)) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = label,
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = "입력 ${formatTokenCount(stat.inputTokens)}",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.padding(start = 8.dp),
            )
        }
        Spacer(Modifier.height(4.dp))
        Box(
            Modifier.fillMaxWidth().height(6.dp)
                .background(denebHairline(), RoundedCornerShape(3.dp)),
        ) {
            Box(
                Modifier.fillMaxWidth(fraction).height(6.dp)
                    .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(3.dp)),
            )
        }
        Text(
            text = usageSubLine(stat),
            style = DenebType.meta,
            color = denebHint(),
            modifier = Modifier.padding(top = 2.dp),
        )
    }
}

private fun usageSubLine(stat: UsageStat): String {
    val base = "${stat.runs}회 · 출력 ${formatTokenCount(stat.outputTokens)}"
    return if (stat.cacheReadTokens > 0) "$base · 캐시 ${formatTokenCount(stat.cacheReadTokens)}" else base
}

/** Localize a model-role slug to Korean; unknown slugs pass through. */
private fun usageRoleLabel(role: String): String = when (role) {
    "main" -> "메인"
    "submain" -> "서브메인"
    "main2" -> "메인2"
    "coding" -> "코딩"
    "lightweight" -> "경량"
    "tiny" -> "초경량"
    "fallback" -> "폴백"
    "vision" -> "비전"
    else -> role
}

/** Localize a work-type slug (from the gateway's session-key classifier). */
private fun usageWorkTypeLabel(work: String): String = when (work) {
    "heartbeat" -> "하트비트"
    "phone-event" -> "폰 이벤트"
    "chat" -> "대화"
    "subagent" -> "하위 에이전트"
    "mail-analysis" -> "메일 분석"
    "mail-qa" -> "메일 QA"
    "cron" -> "정기 보고"
    "wiki-background" -> "위키 배경"
    "noti-digest" -> "알림 정리"
    "supernote-digest" -> "수기노트 정리"
    "skill-review" -> "스킬 리뷰"
    "groupware-radar" -> "그룹웨어"
    "dream" -> "드림"
    "boot" -> "부팅 점검"
    "system" -> "시스템"
    "other" -> "기타"
    else -> work
}
