package ai.deneb.deneb

import ai.deneb.deneb.generated.ProjectDigestRow
import ai.deneb.deneb.generated.ProjectDigestsOut
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
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
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Instant

/**
 * 프로젝트 진행상황 — a 모아보기 of each active project's LATEST progress
 * (`miniapp.project.digests`). Unlike 카테고리 (which lists every page of a project,
 * the full state), this is the chief-of-staff's glance: one card per project with
 * the current one-line headline + the two or three things that actually moved
 * recently, newest-active project first. The digests are written offline by the
 * wiki dream cycle, so the screen loads instantly from disk; pull to refresh just
 * re-reads. Tapping a card opens that project's pages (프로젝트/<name> category).
 *
 * Design split (see .claude/rules/native-design-system.md): frame + type are the
 * Deneb skin (DenebScreenScaffold + DenebType + grouped DenebGroup cards); the
 * pull-to-refresh is Material. The card list is a stateless body
 * ([ProjectDigestContent]) the render harness can preview with mock data; this
 * composable is the stateful shell (fetch + loading/error/empty states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebProjectDigestScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenProject: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var digests by remember { mutableStateOf<List<ProjectDigestRow>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebDashboardScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched: ProjectDigestsOut? = client.fetchProjectDigests()
        if (fetched == null) {
            loadOk = false
        } else {
            digests = fetched.digests
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "프로젝트 진행상황", onBack = onBack, tabBar = navigationTabBar) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = {
                scope.launch {
                    refreshing = true
                    load()
                    refreshing = false
                }
            },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                when {
                    loadOk == null && digests.isEmpty() -> DenebLoading()

                    loadOk == false && digests.isEmpty() -> DenebError(
                        "프로젝트 진행상황을 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    // No digest yet (the dream cycle hasn't rolled any project up).
                    digests.isEmpty() -> DenebEmpty("아직 모인 프로젝트 진행상황이 없습니다.")

                    else -> ProjectDigestContent(digests, onOpenProject)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The digest cards: one grouped card per project, newest-active first. Each card
 * is a tappable header (project name + "as of" date + chevron) over the current
 * headline and a few recent-change bullets, plus any imminent due. Pure
 * presentation — the shell owns fetch + state.
 */
@Composable
internal fun ProjectDigestContent(digests: List<ProjectDigestRow>, onOpenProject: (String) -> Unit) {
    val tz = remember { TimeZone.currentSystemDefault() }
    Column(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        digests.forEach { d ->
            ProjectDigestCard(d = d, tz = tz, onOpen = { onOpenProject(d.project) })
            Spacer(Modifier.height(18.dp))
        }
    }
}

/** One project's digest card: a pressable header (name + date + chevron) over the
 *  headline and recent-change bullets. The header tap opens the project's pages. */
@Composable
private fun ProjectDigestCard(d: ProjectDigestRow, tz: TimeZone, onOpen: () -> Unit) {
    val haptics = rememberHaptics()
    DenebGroup {
        // Header: project name + "기준" date + a drill chevron. Whole header pressable.
        Row(
            Modifier
                .fillMaxWidth()
                .denebPressable(onClick = {
                    haptics.tap()
                    onOpen()
                })
                .padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = d.project.ifBlank { "(이름 없음)" },
                style = DenebType.cardTitle,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            val stamp = projectDigestDateLabel(d.updatedAtMs, tz)
            if (stamp.isNotEmpty()) {
                Text(
                    text = stamp,
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            Text(
                text = "›",
                style = DenebType.meta,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(start = 8.dp),
            )
        }
        // Headline: the one-line current status.
        if (d.headline.isNotBlank()) {
            Text(
                text = d.headline,
                style = DenebType.rowTitle,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 2.dp),
            )
        }
        // Recent-change bullets.
        d.bullets.forEach { b ->
            Text(
                text = "• $b",
                style = DenebType.rowSubtitle,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 4.dp),
            )
        }
        // Imminent due, when stated.
        if (d.due.isNotBlank()) {
            Text(
                text = "마감 ${d.due}",
                style = DenebType.meta,
                color = denebHint(),
                modifier = Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 6.dp),
            )
        }
        // Bottom padding for the card (DenebGroup adds none of its own).
        Spacer(Modifier.height(16.dp))
    }
}

/**
 * "기준 date" label for a digest ("6월 20일 기준"). Day granularity — a digest is a
 * snapshot, not a clock time. Blank for a missing/zero timestamp so the header
 * omits the stamp.
 */
internal fun projectDigestDateLabel(epochMs: Long, tz: TimeZone): String {
    if (epochMs <= 0L) return ""
    val local = runCatching { Instant.fromEpochMilliseconds(epochMs).toLocalDateTime(tz) }.getOrNull() ?: return ""
    return "${local.month.ordinal + 1}월 ${local.day}일 기준"
}
