package ai.deneb.deneb

import ai.deneb.deneb.generated.ProjectSiteRow
import ai.deneb.deneb.generated.ProjectSitesOut
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Matrix
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.daysUntil
import kotlinx.datetime.todayIn
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt
import kotlin.time.Clock

/**
 * 현장 지도 — plots each active project's 현장(sites) onto a map of Korea (the mobile
 * port of the andromeda desktop pane). It reads `miniapp.project.sites` (every active
 * 대표페이지 carrying Sites, whether or not it has a 현재 상태 digest) and places each
 * site by the finest administrative unit it names: 읍/면 → 시군구 → 시도.
 *
 * A pin encodes three business dimensions from the project's wiki Meta — the same
 * mapping the desktop uses, so both clients read a site identically:
 *   색  = 에너지원 (Kinds 상위: 태양광/풍력/기자재/기타)
 *   모양 = 특성    (Kinds 하위: 토지=원 / 루프탑=사각 / 수상=마름모 / 기타=삼각)
 *   크기 = 용량    (Capacity, MW — √ scale)
 *
 * Filter by 에너지원 or 특성; tap a pin (or a list row) for the detail sheet. A site
 * that names no known 시도 lands in the 미배치 tray, not silently dropped.
 *
 * Design split (docs/agent-rules/native-design-system.md): frame + type are the Deneb
 * skin (DenebScreenScaffold + DenebType + DenebChip); the map is a Compose Canvas that
 * parses the 시도 outline paths from [KoreaGeo]; pull-to-refresh + bottom sheet are
 * Material. The stateless [SiteMapContent] body is previewable (RenderPreviewScreens).
 */

// 에너지원 categorical palette. Data-viz category colors are a legitimate hardcoded-hex
// exception to the monochrome doctrine (native-design-system §색): chrome/neutrals still
// come from MaterialTheme.colorScheme, but the four sources need stable, mutually
// distinct hues that survive both light and dark.
private val sourceSolar = Color(0xFFE0A030) // 태양광 — warm amber
private val sourceWind = Color(0xFF3AA6A0) // 풍력 — teal
private val sourceEquip = Color(0xFF6B7A99) // 기자재 — slate
private val sourceEtc = Color(0xFF9AA0A8) // 기타 — gray

private val sourceOrder = listOf("태양광", "풍력", "기자재", "기타")

private fun sourceColor(source: String): Color = when (source) {
    "태양광" -> sourceSolar
    "풍력" -> sourceWind
    "기자재" -> sourceEquip
    else -> sourceEtc
}

// 특성 → shape. The 태양광 site types the operator named drive the mark; any other
// sub-kind (ESS/육상/해상/모듈…) falls to 기타 (triangle).
private val typeShape = mapOf("토지" to "circle", "루프탑" to "square", "수상" to "diamond")
private val typeOrder = listOf("토지", "루프탑", "수상", "기타")

private fun shapeOfType(type: String): String = typeShape[type] ?: "triangle"
private fun typeLabel(type: String): String = if (typeShape.containsKey(type)) type else "기타"

private fun capacityText(mw: Double): String {
    if (mw <= 0.0) return "미기재"
    val rounded = (mw * 10).toLong() / 10.0
    return if (rounded == rounded.toLong().toDouble()) "${rounded.toLong()}MW" else "${rounded}MW"
}

// 공정 일정 — the 현장 공통 포맷 milestone dates, in process order. Rendered as a
// timeline in the detail sheet; the two 검사일 also drive 임박 검사 surfacing.
internal data class Sched(
    val contractDate: String,
    val constructionStart: String,
    val moduleDelivery: String,
    val preUseInspection: String,
    val completionInspection: String,
) {
    fun anyFilled(): Boolean = contractDate.isNotEmpty() || constructionStart.isNotEmpty() || moduleDelivery.isNotEmpty() ||
        preUseInspection.isNotEmpty() || completionInspection.isNotEmpty()
}

private data class Milestone(val label: String, val date: String, val inspection: Boolean)

private fun Sched.milestones(): List<Milestone> = listOf(
    Milestone("계약", contractDate, false),
    Milestone("공사개시", constructionStart, false),
    Milestone("모듈입고", moduleDelivery, false),
    Milestone("사용전검사", preUseInspection, true),
    Milestone("준공검사", completionInspection, true),
)

// Parse a YYYY-MM-DD milestone date, else null. 모듈입고 may be a free-form 기간 —
// those simply don't parse and show as-is in the timeline without a D-day.
private fun parseYmd(s: String): LocalDate? = runCatching { LocalDate.parse(s.trim()) }.getOrNull()

// The nearest not-yet-past 검사일 (사용전/준공) — the "임박 검사일" the map surfaces so an
// inspection never sneaks up. null when both are blank or already past.
private data class Inspection(val label: String, val date: String, val days: Int)

private fun upcomingInspection(sched: Sched, today: LocalDate): Inspection? {
    var best: Inspection? = null
    for ((label, date) in listOf("사용전검사" to sched.preUseInspection, "준공검사" to sched.completionInspection)) {
        val d = parseYmd(date) ?: continue
        val days = today.daysUntil(d)
        if (days < 0) continue
        if (best == null || days < best.days) best = Inspection(label, date, days)
    }
    return best
}

// 임박 = within IMMINENT_DAYS. Drives the header count + the error-hued D-day badge.
private const val IMMINENT_DAYS = 30
private fun ddayText(days: Int): String = if (days == 0) "D-day" else "D-$days"

// A project's Kinds are "상위/하위" (e.g. "태양광/루프탑"); the primary kind is the first
// entry. Split it into 에너지원 (source) and 특성 (type).
private fun primaryKind(kinds: List<String>): Pair<String, String> {
    val parts = (kinds.firstOrNull() ?: "").split("/")
    return (parts.getOrNull(0) ?: "") to (parts.getOrNull(1) ?: "")
}

// Pin radius (dp) from 용량(MW): sqrt so a 100MW farm isn't 100× a 1MW rooftop, just
// visibly larger. Unrecorded (0) draws at a small base so it's still placeable.
private fun radiusDpOf(capacity: Double): Float {
    if (capacity <= 0.0) return 3.5f
    return min(3.0 + sqrt(capacity) * 0.85, 11.0).toFloat()
}

// Resolve a site string to KoreaGeo viewBox coords, finest first: 읍/면 → 시군구 → 시도,
// else null (unplaceable). Warded cities are keyed combined ("경기|성남시분당구").
private fun resolveSite(site: String): FloatArray? {
    val t = site.trim().split(Regex("\\s+"))
    val sido = t.getOrNull(0)?.takeIf { it.isNotEmpty() } ?: return null
    var sgg: String? = null
    var dong: String? = null
    val t1 = t.getOrNull(1)
    val t2 = t.getOrNull(2)
    if (t1 != null && t2 != null && KoreaGeo.sigungu.containsKey("$sido|$t1$t2")) {
        sgg = "$t1$t2"
        dong = t.getOrNull(3)
    } else if (t1 != null && KoreaGeo.sigungu.containsKey("$sido|$t1")) {
        sgg = t1
        dong = t.getOrNull(2)
    }
    if (sgg != null && dong != null) {
        KoreaGeo.eupmyeon["$sido|$sgg|$dong"]?.let { return it }
    }
    if (sgg != null) return KoreaGeo.sigungu["$sido|$sgg"]
    return KoreaGeo.provinceCentroid[sido]
}

/** Shared detail fields for placed pins and unplaced rows — drives the bottom sheet. */
private data class SiteDetail(
    val site: String,
    val project: String,
    val client: String,
    val path: String,
    val source: String,
    val type: String,
    val capacity: Double,
    val status: String,
    val due: String,
    val kinds: List<String>,
    val sched: Sched,
)

/** One placed pin — its viewBox coords plus the resolved encoding + detail fields. */
private data class SitePin(
    val vx: Float,
    val vy: Float,
    val radiusDp: Float,
    val detail: SiteDetail,
) {
    val site get() = detail.site
    val project get() = detail.project
    val client get() = detail.client
    val path get() = detail.path
    val source get() = detail.source
    val type get() = detail.type
    val capacity get() = detail.capacity
    val status get() = detail.status
    val due get() = detail.due
    val kinds get() = detail.kinds
    val sched get() = detail.sched
}

// Lifecycle: 후보 → 계약 → 개설 → 준공. Default = 개설 only (공사중). Everything
// else (계약·준공·후보·미분류 "") is gated behind an "… 포함" chip so the map
// opens on active construction sites. 대표페이지 fallback rows carry status ""
// (미분류) — they used to always show, which flooded the map before 현장 pages
// carried a real status.
private const val STATUS_UNDER_CONSTRUCTION = "개설"
private const val STATUS_CONTRACT = "계약"
private const val STATUS_COMPLETED = "준공"
private const val STATUS_PROSPECTIVE = "후보"

// Lifecycle choices for the detail-sheet editor ("" = 미분류).
private data class StatusChoice(val value: String, val label: String)

private val STATUS_CHOICES = listOf(
    StatusChoice(STATUS_PROSPECTIVE, "후보"),
    StatusChoice(STATUS_CONTRACT, "계약"),
    StatusChoice(STATUS_UNDER_CONSTRUCTION, "개설"),
    StatusChoice(STATUS_COMPLETED, "준공"),
    StatusChoice("", "미분류"),
)

/** Path-shape check for 프로젝트/<name>/현장/<site>.md — editable status surface. */
private fun isSitePagePath(path: String): Boolean {
    val parts = path.trim().replace('\\', '/').split('/')
    return parts.size == 4 && parts[0] == "프로젝트" && parts[2] == "현장" && parts[3].endsWith(".md")
}

private fun statusVisible(
    status: String,
    showContracted: Boolean,
    showCompleted: Boolean,
    showProspective: Boolean,
    showUnclassified: Boolean,
): Boolean = when (status) {
    STATUS_UNDER_CONSTRUCTION -> true
    STATUS_CONTRACT -> showContracted
    STATUS_COMPLETED -> showCompleted
    STATUS_PROSPECTIVE -> showProspective
    else -> showUnclassified // "" and any unknown label
}

// An unplaceable 현장 — same detail surface as a pin so the 미배치 tray can open
// the full bottom sheet and apply the same status gate.
private typealias UnplacedSite = SiteDetail

private data class Placed(val pins: List<SitePin>, val unplaced: List<UnplacedSite>)

private fun schedOf(r: ProjectSiteRow): Sched = Sched(
    contractDate = r.contract_date.trim(),
    constructionStart = r.construction_start.trim(),
    moduleDelivery = r.module_delivery.trim(),
    preUseInspection = r.pre_use_inspection.trim(),
    completionInspection = r.completion_inspection.trim(),
)

private fun siteDetail(
    site: String,
    r: ProjectSiteRow,
    source: String,
    type: String,
    status: String,
    sched: Sched,
): SiteDetail = SiteDetail(
    site = site,
    project = r.project,
    client = r.client,
    path = r.path,
    source = source,
    type = type,
    capacity = r.capacity,
    status = status,
    due = r.due,
    kinds = r.kinds,
    sched = sched,
)

/** After a status write, auto-enable the matching "… 포함" chip so the pin stays visible. */
private fun revealFilterForStatus(
    status: String,
    showContracted: (Boolean) -> Unit,
    showCompleted: (Boolean) -> Unit,
    showProspective: (Boolean) -> Unit,
    showUnclassified: (Boolean) -> Unit,
) {
    when (status) {
        STATUS_CONTRACT -> showContracted(true)
        STATUS_COMPLETED -> showCompleted(true)
        STATUS_PROSPECTIVE -> showProspective(true)
        STATUS_UNDER_CONSTRUCTION -> Unit
        else -> showUnclassified(true)
    }
}

private fun placeSites(rows: List<ProjectSiteRow>): Placed {
    val pins = mutableListOf<SitePin>()
    val unplaced = mutableListOf<UnplacedSite>()
    val seen = mutableMapOf<String, Int>()
    for (r in rows) {
        val (source, type) = primaryKind(r.kinds)
        val rad = radiusDpOf(r.capacity)
        val status = r.status.trim()
        val sched = schedOf(r)
        // A 현장 page with no address yet (empty sites) still surfaces — as a 미배치 row.
        if (r.sites.isEmpty()) {
            unplaced.add(siteDetail("(주소 미기재)", r, source, type, status, sched))
            continue
        }
        for (site in r.sites) {
            val xy = resolveSite(site)
            val rowDetail = siteDetail(site, r, source, type, status, sched)
            if (xy == null) {
                unplaced.add(rowDetail)
                continue
            }
            val key = "${xy[0]},${xy[1]}"
            val n = seen.getOrElse(key) { 0 }
            seen[key] = n + 1
            val ang = n * 2.399
            val spread = if (n == 0) 0.0 else rad + 4 + n * 1.5
            pins.add(
                SitePin(
                    vx = (xy[0] + cos(ang) * spread).toFloat(),
                    vy = (xy[1] + sin(ang) * spread).toFloat(),
                    radiusDp = rad,
                    detail = rowDetail,
                ),
            )
        }
    }
    return Placed(pins, unplaced)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebSiteMapScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenProject: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    var rows by remember { mutableStateOf<List<ProjectSiteRow>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed (mirrors DenebProjectDigestScreen).
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    suspend fun load() {
        val fetched: ProjectSitesOut? = client.fetchProjectSites()
        if (fetched == null) {
            loadOk = false
        } else {
            rows = fetched.sites
            loadOk = true
        }
    }
    LaunchedEffect(Unit) { load() }

    DenebScreenScaffold(title = "현장 지도", onBack = onBack, tabBar = navigationTabBar) {
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
                when {
                    loadOk == null && rows.isEmpty() -> DenebLoading()

                    loadOk == false && rows.isEmpty() -> DenebError(
                        "현장 지도를 불러오지 못했습니다.",
                        onRetry = {
                            scope.launch {
                                loadOk = null
                                load()
                            }
                        },
                    )

                    rows.isEmpty() -> DenebEmpty("현장이 있는 프로젝트가 없습니다.")

                    else -> SiteMapContent(
                        rows = rows,
                        onOpenProject = onOpenProject,
                        setSiteStatus = { path, status -> client.setProjectSiteStatus(path, status)?.status },
                        ensureSite = { path, address ->
                            client.ensureProjectSite(path, address)?.let { it.path to it.status }
                        },
                        updateSite = { path, contractDate, constructionStart, moduleDelivery, preUseInspection, completionInspection ->
                            client.updateProjectSite(
                                path,
                                contractDate,
                                constructionStart,
                                moduleDelivery,
                                preUseInspection,
                                completionInspection,
                            )?.let { out ->
                                Sched(
                                    contractDate = out.contract_date.trim(),
                                    constructionStart = out.construction_start.trim(),
                                    moduleDelivery = out.module_delivery.trim(),
                                    preUseInspection = out.pre_use_inspection.trim(),
                                    completionInspection = out.completion_inspection.trim(),
                                )
                            }
                        },
                        onSitesReload = { load() },
                    )
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless body (previewable) ----------------------------------------

/**
 * The map + filters + list. Pure presentation over an already-fetched [rows]; the
 * shell owns fetch + loading/error/empty. Filter + selection state is local UI state.
 * [setSiteStatus] writes a 현장 page lifecycle and returns the normalized status
 * (null on failure); [ensureSite] finds or creates a 현장 page for an address;
 * [updateSite] patches milestone dates; [onSitesReload] refreshes the parent list.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
internal fun SiteMapContent(
    rows: List<ProjectSiteRow>,
    onOpenProject: (String) -> Unit = {},
    setSiteStatus: (suspend (path: String, status: String) -> String?)? = null,
    ensureSite: (suspend (path: String, address: String) -> Pair<String, String>?)? = null,
    updateSite: (
        suspend (
            path: String,
            contractDate: String,
            constructionStart: String,
            moduleDelivery: String,
            preUseInspection: String,
            completionInspection: String,
        ) -> Sched?
    )? = null,
    onSitesReload: (suspend () -> Unit)? = null,
) {
    val placed = remember(rows) { placeSites(rows) }
    val pins = placed.pins
    val unplaced = placed.unplaced
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()

    var sourceFilter by remember { mutableStateOf<Set<String>>(emptySet()) }
    var typeFilter by remember { mutableStateOf<Set<String>>(emptySet()) }
    var showContracted by remember { mutableStateOf(false) }
    var showCompleted by remember { mutableStateOf(false) }
    var showProspective by remember { mutableStateOf(false) }
    var showUnclassified by remember { mutableStateOf(false) }
    var selected by remember { mutableStateOf<SiteDetail?>(null) }

    fun select(detail: SiteDetail) {
        haptics.tap()
        selected = detail
    }

    val sourcesPresent = remember(pins) {
        val present = pins.mapNotNull { it.source.takeIf { s -> s.isNotEmpty() } }.toSet()
        sourceOrder.filter { it in present } + present.filter { it !in sourceOrder }
    }
    val typesPresent = remember(pins) {
        val present = pins.map { typeLabel(it.type) }.toSet()
        typeOrder.filter { it in present }
    }

    val statusCounts = remember(pins, unplaced) {
        fun count(want: (String) -> Boolean) = pins.count { want(it.status) } + unplaced.count { want(it.status) }
        StatusCounts(
            contracted = count { it == STATUS_CONTRACT },
            completed = count { it == STATUS_COMPLETED },
            prospective = count { it == STATUS_PROSPECTIVE },
            unclassified = count {
                it != STATUS_UNDER_CONSTRUCTION &&
                    it != STATUS_CONTRACT &&
                    it != STATUS_COMPLETED &&
                    it != STATUS_PROSPECTIVE
            },
        )
    }

    val shown = remember(pins, sourceFilter, typeFilter, showContracted, showCompleted, showProspective, showUnclassified) {
        pins.filter {
            statusVisible(it.status, showContracted, showCompleted, showProspective, showUnclassified) &&
                (sourceFilter.isEmpty() || it.source in sourceFilter) &&
                (typeFilter.isEmpty() || typeLabel(it.type) in typeFilter)
        }
    }
    // 미배치 applies the same status gate so a hidden site never leaks in.
    val shownUnplaced = remember(unplaced, showContracted, showCompleted, showProspective, showUnclassified) {
        unplaced.filter {
            statusVisible(it.status, showContracted, showCompleted, showProspective, showUnclassified)
        }
    }
    val totalMw = remember(shown) { shown.sumOf { it.capacity } }
    // Re-derive "today" whenever the fetched rows change (keyed on [rows]) so a
    // pull-to-refresh the next day advances the D-day baseline — remembering it forever
    // would freeze the imminent-inspection surfacing at the day the screen first mounted.
    val today = remember(rows) { Clock.System.todayIn(TimeZone.currentSystemDefault()) }
    // 임박 검사 — how many shown 현장 have a 검사일 within IMMINENT_DAYS. Unplaced 현장 count
    // too: an approaching 검사 must not be hidden just because the address doesn't resolve.
    val imminentCount = remember(shown, shownUnplaced, today) {
        fun imminent(s: Sched): Boolean {
            val up = upcomingInspection(s, today)
            return up != null && up.days <= IMMINENT_DAYS
        }
        shown.count { imminent(it.sched) } + shownUnplaced.count { imminent(it.sched) }
    }

    val selectedPin = remember(selected, pins) {
        selected?.let { d ->
            pins.find { it.site == d.site && it.path == d.path && it.project == d.project }
        }
    }

    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        // Summary line
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("현장 ${shown.size}", style = DenebType.rowTitleStrong)
            if (totalMw > 0) {
                Spacer(Modifier.width(8.dp))
                Text("총 ${capacityText(totalMw)}", style = DenebType.meta, color = denebHint())
            }
            if (shownUnplaced.isNotEmpty()) {
                Spacer(Modifier.width(8.dp))
                Text("미배치 ${shownUnplaced.size}", style = DenebType.meta, color = denebHint())
            }
            if (imminentCount > 0) {
                Spacer(Modifier.width(8.dp))
                Text("임박검사 $imminentCount", style = DenebType.meta, color = MaterialTheme.colorScheme.error)
            }
        }

        Spacer(Modifier.height(10.dp))

        // Filters — 에너지원 + 특성 chips
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            sourcesPresent.forEach { s ->
                DenebChip(
                    selected = s in sourceFilter,
                    onClick = { sourceFilter = sourceFilter.toggle(s) },
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(9.dp).background(sourceColor(s), RoundedCornerShape(50)))
                        Spacer(Modifier.width(6.dp))
                        Text(s, style = DenebType.meta)
                    }
                }
            }
            typesPresent.forEach { t ->
                DenebChip(
                    selected = t in typeFilter,
                    onClick = { typeFilter = typeFilter.toggle(t) },
                ) {
                    Text(t, style = DenebType.meta)
                }
            }
            if (statusCounts.contracted > 0) {
                DenebChip(selected = showContracted, onClick = { showContracted = !showContracted }) {
                    Text("계약 포함 ${statusCounts.contracted}", style = DenebType.meta)
                }
            }
            if (statusCounts.completed > 0) {
                DenebChip(selected = showCompleted, onClick = { showCompleted = !showCompleted }) {
                    Text("준공 포함 ${statusCounts.completed}", style = DenebType.meta)
                }
            }
            if (statusCounts.prospective > 0) {
                DenebChip(selected = showProspective, onClick = { showProspective = !showProspective }) {
                    Text("후보 포함 ${statusCounts.prospective}", style = DenebType.meta)
                }
            }
            if (statusCounts.unclassified > 0) {
                DenebChip(selected = showUnclassified, onClick = { showUnclassified = !showUnclassified }) {
                    Text("미분류 포함 ${statusCounts.unclassified}", style = DenebType.meta)
                }
            }
        }

        Spacer(Modifier.height(12.dp))

        // The map
        SiteMapCanvas(pins = shown, selected = selectedPin, onPinTap = { select(it.detail) })

        Spacer(Modifier.height(10.dp))
        SiteMapLegend(sourcesPresent, typesPresent)

        Spacer(Modifier.height(16.dp))
    }

    // Filtered site list — tap a row for the same detail sheet as a pin.
    if (shown.isNotEmpty()) {
        DenebSectionLabel("현장 목록")
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            shown.sortedByDescending { it.capacity }.forEach { pin ->
                SiteRow(pin, today) { select(pin.detail) }
            }
        }
    }

    if (shownUnplaced.isNotEmpty()) {
        Spacer(Modifier.height(16.dp))
        DenebSectionLabel("미배치 — 주소를 지도에 매칭하지 못한 현장")
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
            shownUnplaced.forEach { u ->
                UnplacedRow(u, today) { select(u) }
            }
        }
    }

    // Detail sheet (B3): 거래처/현장/상태(편집)/에너지원/특성/용량/마감 + 위키 열기.
    selected?.let { detail ->
        val sheetState = rememberModalBottomSheetState()
        var statusBusy by remember(detail.path) { mutableStateOf(false) }
        var statusError by remember(detail.path) { mutableStateOf<String?>(null) }
        var ensureBusy by remember(detail.path) { mutableStateOf(false) }
        var ensureError by remember(detail.path) { mutableStateOf<String?>(null) }
        var pendingStatus by remember { mutableStateOf<String?>(null) }
        var showDateConfirm by remember { mutableStateOf(false) }
        var dateConfirmMessage by remember { mutableStateOf("") }

        suspend fun applyStatusChange(newStatus: String, setDateToday: Boolean) {
            statusBusy = true
            statusError = null
            val next = setSiteStatus?.invoke(detail.path, newStatus)
            if (next == null) {
                statusError = "상태 변경에 실패했습니다."
                statusBusy = false
                return
            }
            var newSched = detail.sched
            if (setDateToday && updateSite != null) {
                val todayStr = today.toString()
                newSched = when (newStatus) {
                    STATUS_CONTRACT -> detail.sched.copy(contractDate = todayStr)
                    STATUS_UNDER_CONSTRUCTION -> detail.sched.copy(constructionStart = todayStr)
                    else -> detail.sched
                }
                val updated = updateSite.invoke(
                    detail.path,
                    newSched.contractDate,
                    newSched.constructionStart,
                    newSched.moduleDelivery,
                    newSched.preUseInspection,
                    newSched.completionInspection,
                )
                if (updated != null) newSched = updated
            }
            revealFilterForStatus(
                next,
                { showContracted = it },
                { showCompleted = it },
                { showProspective = it },
                { showUnclassified = it },
            )
            selected = detail.copy(status = next, sched = newSched)
            onSitesReload?.invoke()
            statusBusy = false
        }

        fun requestStatusChange(choice: StatusChoice) {
            if (statusBusy || detail.status == choice.value) return
            when {
                choice.value == STATUS_CONTRACT &&
                    detail.sched.contractDate.isEmpty() &&
                    updateSite != null -> {
                    pendingStatus = choice.value
                    dateConfirmMessage = "계약일을 오늘로 넣을까요?"
                    showDateConfirm = true
                }

                choice.value == STATUS_UNDER_CONSTRUCTION &&
                    detail.sched.constructionStart.isEmpty() &&
                    updateSite != null -> {
                    pendingStatus = choice.value
                    dateConfirmMessage = "공사개시일을 오늘로 넣을까요?"
                    showDateConfirm = true
                }

                else -> scope.launch { applyStatusChange(choice.value, setDateToday = false) }
            }
        }

        if (showDateConfirm) {
            AlertDialog(
                onDismissRequest = {
                    showDateConfirm = false
                    pendingStatus = null
                },
                title = { Text("일정 확인", style = DenebType.rowTitle) },
                text = { Text(dateConfirmMessage, style = DenebType.body) },
                confirmButton = {
                    TextButton(
                        onClick = {
                            val status = pendingStatus
                            showDateConfirm = false
                            pendingStatus = null
                            if (status != null) scope.launch { applyStatusChange(status, setDateToday = true) }
                        },
                    ) { Text("예", style = DenebType.button) }
                },
                dismissButton = {
                    TextButton(
                        onClick = {
                            val status = pendingStatus
                            showDateConfirm = false
                            pendingStatus = null
                            if (status != null) scope.launch { applyStatusChange(status, setDateToday = false) }
                        },
                    ) { Text("아니오", style = DenebType.button) }
                },
            )
        }

        ModalBottomSheet(onDismissRequest = { selected = null }, sheetState = sheetState) {
            Column(Modifier.fillMaxWidth().padding(horizontal = 24.dp).padding(bottom = 24.dp)) {
                Text(detail.project, style = DenebType.subject)
                Spacer(Modifier.height(12.dp))
                if (detail.client.isNotEmpty()) DetailRow("거래처", detail.client)
                DetailRow("현장", detail.site)
                if (isSitePagePath(detail.path) && setSiteStatus != null) {
                    Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
                        Text("상태", style = DenebType.meta, color = denebHint())
                        Spacer(Modifier.height(6.dp))
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            STATUS_CHOICES.forEach { choice ->
                                DenebChip(
                                    selected = detail.status == choice.value,
                                    enabled = !statusBusy,
                                    onClick = { requestStatusChange(choice) },
                                ) {
                                    Text(choice.label, style = DenebType.meta)
                                }
                            }
                        }
                        if (statusBusy) {
                            Spacer(Modifier.height(6.dp))
                            Text("저장 중…", style = DenebType.meta, color = denebHint())
                        }
                        statusError?.let { err ->
                            Spacer(Modifier.height(6.dp))
                            Text(err, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
                        }
                    }
                } else {
                    DetailRow("상태", detail.status.ifEmpty { "미분류" })
                }
                if (
                    !isSitePagePath(detail.path) &&
                    detail.site != "(주소 미기재)" &&
                    ensureSite != null
                ) {
                    TextButton(
                        enabled = !ensureBusy,
                        onClick = {
                            scope.launch {
                                ensureBusy = true
                                ensureError = null
                                val result = ensureSite(detail.path, detail.site)
                                if (result == null) {
                                    ensureError = "현장 페이지를 만들지 못했습니다."
                                } else {
                                    val (newPath, newStatus) = result
                                    revealFilterForStatus(
                                        newStatus,
                                        { showContracted = it },
                                        { showCompleted = it },
                                        { showProspective = it },
                                        { showUnclassified = it },
                                    )
                                    selected = detail.copy(path = newPath, status = newStatus)
                                    onSitesReload?.invoke()
                                }
                                ensureBusy = false
                            }
                        },
                    ) {
                        Text(
                            if (ensureBusy) "만드는 중…" else "현장 페이지 만들기",
                            style = DenebType.button,
                        )
                    }
                    ensureError?.let { err ->
                        Text(err, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
                    }
                }
                if (detail.source.isNotEmpty()) DetailRow("에너지원", detail.source)
                if (detail.type.isNotEmpty()) DetailRow("특성", typeLabel(detail.type))
                DetailRow("용량", capacityText(detail.capacity))
                DetailRow("마감", detail.due.ifEmpty { "미정" })
                if (isSitePagePath(detail.path) && updateSite != null) {
                    EditableScheduleSection(
                        path = detail.path,
                        sched = detail.sched,
                        today = today,
                        enabled = !statusBusy,
                        updateSite = updateSite,
                        onSchedUpdated = { updated -> selected = detail.copy(sched = updated) },
                        onReload = { scope.launch { onSitesReload?.invoke() } },
                        scope = scope,
                    )
                } else {
                    ScheduleTimeline(detail.sched, today)
                }
                if (detail.path.isNotEmpty()) {
                    Spacer(Modifier.height(8.dp))
                    TextButton(onClick = {
                        val path = detail.path
                        selected = null
                        onOpenProject(path)
                    }) {
                        Text("위키 열기", style = DenebType.button)
                    }
                }
            }
        }
    }
}

private data class StatusCounts(
    val contracted: Int,
    val completed: Int,
    val prospective: Int,
    val unclassified: Int,
)

private fun Set<String>.toggle(v: String): Set<String> = if (v in this) this - v else this + v

@Composable
private fun DetailRow(label: String, value: String) {
    Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        Text(label, style = DenebType.meta, color = denebHint())
        Spacer(Modifier.height(2.dp))
        Text(value, style = DenebType.body)
    }
}

@Composable
private fun UnplacedRow(detail: SiteDetail, today: LocalDate, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val color = sourceColor(detail.source)
        Canvas(Modifier.size(14.dp)) {
            drawMark(shapeOfType(detail.type), Offset(size.width / 2, size.height / 2), 5.dp.toPx(), color)
        }
        Spacer(Modifier.width(8.dp))
        Column(Modifier.weight(1f)) {
            Text(detail.site, style = DenebType.rowTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                buildString {
                    append(detail.project)
                    if (detail.client.isNotEmpty()) append(" · ${detail.client}")
                },
                style = DenebType.meta,
                color = denebHint(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.width(8.dp))
        InspectionBadge(detail.sched, today)
        Text(
            listOf(detail.source, capacityText(detail.capacity)).filter { it.isNotEmpty() }.joinToString(" · "),
            style = DenebType.meta,
            color = denebHint(),
        )
    }
}

@Composable
private fun EditableScheduleSection(
    path: String,
    sched: Sched,
    today: LocalDate,
    enabled: Boolean,
    updateSite: suspend (
        path: String,
        contractDate: String,
        constructionStart: String,
        moduleDelivery: String,
        preUseInspection: String,
        completionInspection: String,
    ) -> Sched?,
    onSchedUpdated: (Sched) -> Unit,
    onReload: () -> Unit,
    scope: CoroutineScope,
) {
    var contractDate by remember(sched) { mutableStateOf(sched.contractDate) }
    var constructionStart by remember(sched) { mutableStateOf(sched.constructionStart) }
    var moduleDelivery by remember(sched) { mutableStateOf(sched.moduleDelivery) }
    var preUseInspection by remember(sched) { mutableStateOf(sched.preUseInspection) }
    var completionInspection by remember(sched) { mutableStateOf(sched.completionInspection) }
    var schedBusy by remember { mutableStateOf(false) }
    var schedError by remember { mutableStateOf<String?>(null) }

    fun save() {
        scope.launch {
            schedBusy = true
            schedError = null
            val updated = updateSite(
                path,
                contractDate.trim(),
                constructionStart.trim(),
                moduleDelivery.trim(),
                preUseInspection.trim(),
                completionInspection.trim(),
            )
            if (updated == null) {
                schedError = "공정 일정 저장에 실패했습니다."
            } else {
                contractDate = updated.contractDate
                constructionStart = updated.constructionStart
                moduleDelivery = updated.moduleDelivery
                preUseInspection = updated.preUseInspection
                completionInspection = updated.completionInspection
                onSchedUpdated(updated)
                onReload()
            }
            schedBusy = false
        }
    }

    Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        Text("공정 일정", style = DenebType.meta, color = denebHint())
        Spacer(Modifier.height(6.dp))
        DenebOutlinedTextField(
            value = contractDate,
            onValueChange = { contractDate = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = enabled && !schedBusy,
            label = { Text("계약", style = DenebType.meta) },
            placeholder = { Text("YYYY-MM-DD", style = DenebType.hint) },
            singleLine = true,
        )
        Spacer(Modifier.height(6.dp))
        DenebOutlinedTextField(
            value = constructionStart,
            onValueChange = { constructionStart = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = enabled && !schedBusy,
            label = { Text("공사개시", style = DenebType.meta) },
            placeholder = { Text("YYYY-MM-DD", style = DenebType.hint) },
            singleLine = true,
        )
        Spacer(Modifier.height(6.dp))
        DenebOutlinedTextField(
            value = moduleDelivery,
            onValueChange = { moduleDelivery = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = enabled && !schedBusy,
            label = { Text("모듈입고", style = DenebType.meta) },
            placeholder = { Text("기간 또는 날짜", style = DenebType.hint) },
            singleLine = true,
        )
        Spacer(Modifier.height(6.dp))
        DenebOutlinedTextField(
            value = preUseInspection,
            onValueChange = { preUseInspection = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = enabled && !schedBusy,
            label = { Text("사용전검사", style = DenebType.meta) },
            placeholder = { Text("YYYY-MM-DD", style = DenebType.hint) },
            singleLine = true,
        )
        Spacer(Modifier.height(6.dp))
        DenebOutlinedTextField(
            value = completionInspection,
            onValueChange = { completionInspection = it },
            modifier = Modifier.fillMaxWidth(),
            enabled = enabled && !schedBusy,
            label = { Text("준공검사", style = DenebType.meta) },
            placeholder = { Text("YYYY-MM-DD", style = DenebType.hint) },
            singleLine = true,
        )
        val preview = Sched(
            contractDate = contractDate,
            constructionStart = constructionStart,
            moduleDelivery = moduleDelivery,
            preUseInspection = preUseInspection,
            completionInspection = completionInspection,
        )
        if (preview.anyFilled()) {
            Spacer(Modifier.height(8.dp))
            ScheduleTimeline(preview, today, showHeading = false)
        }
        Spacer(Modifier.height(8.dp))
        TextButton(enabled = enabled && !schedBusy, onClick = { save() }) {
            Text(if (schedBusy) "저장 중…" else "저장", style = DenebType.button)
        }
        schedError?.let { err ->
            Text(err, style = DenebType.meta, color = MaterialTheme.colorScheme.error)
        }
    }
}

// 공정 일정 — a small timeline of the five milestone dates in process order. Blank
// milestones stay visible (dimmed) so what's left to fill is obvious; a parseable
// date shows its D-day, past dates read 완료, and the nearest upcoming 검사 is
// error-hued. Renders nothing when the whole schedule is blank (fallback rows).
@Composable
private fun ScheduleTimeline(sched: Sched, today: LocalDate, showHeading: Boolean = true) {
    if (!sched.anyFilled()) return
    val up = upcomingInspection(sched, today)
    Column(Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        if (showHeading) {
            Text("공정 일정", style = DenebType.meta, color = denebHint())
            Spacer(Modifier.height(6.dp))
        }
        sched.milestones().forEach { m ->
            val has = m.date.isNotEmpty()
            val days = if (has) parseYmd(m.date)?.let { today.daysUntil(it) } else null
            val isNextInspection = up != null && m.inspection && m.date == up.date && m.label == up.label
            val done = days != null && days < 0
            val dotColor = when {
                isNextInspection -> MaterialTheme.colorScheme.error
                done -> MaterialTheme.colorScheme.outline
                has -> MaterialTheme.colorScheme.primary
                else -> MaterialTheme.colorScheme.outlineVariant
            }
            Row(
                Modifier.fillMaxWidth().padding(vertical = 3.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(8.dp).background(dotColor, RoundedCornerShape(50)))
                Spacer(Modifier.width(9.dp))
                Text(m.label, style = DenebType.meta, color = denebHint(), modifier = Modifier.width(72.dp))
                Text(m.date.ifEmpty { "미정" }, style = DenebType.body, color = if (has) MaterialTheme.colorScheme.onSurface else denebHint())
                if (days != null) {
                    Spacer(Modifier.width(8.dp))
                    Text(
                        if (done) "완료" else ddayText(days),
                        style = DenebType.meta,
                        color = if (isNextInspection) MaterialTheme.colorScheme.error else denebHint(),
                    )
                }
            }
        }
    }
}

@Composable
private fun SiteRow(pin: SitePin, today: LocalDate, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .padding(vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val color = sourceColor(pin.source)
        Canvas(Modifier.size(14.dp)) {
            drawMark(shapeOfType(pin.type), Offset(size.width / 2, size.height / 2), 5.dp.toPx(), color)
        }
        Spacer(Modifier.width(8.dp))
        Column(Modifier.weight(1f)) {
            Text(pin.site, style = DenebType.rowTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                buildString {
                    append(pin.project)
                    if (pin.client.isNotEmpty()) append(" · ${pin.client}")
                },
                style = DenebType.meta,
                color = denebHint(),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.width(8.dp))
        InspectionBadge(pin.sched, today)
        Text(
            listOf(pin.source, capacityText(pin.capacity)).filter { it.isNotEmpty() }.joinToString(" · "),
            style = DenebType.meta,
            color = denebHint(),
        )
    }
}

// A compact D-day pill for the nearest upcoming 검사일 — error-hued once 임박
// (≤IMMINENT_DAYS), muted otherwise. Renders nothing when no upcoming 검사.
@Composable
private fun InspectionBadge(sched: Sched, today: LocalDate) {
    val up = upcomingInspection(sched, today) ?: return
    val imminent = up.days <= IMMINENT_DAYS
    val tint = if (imminent) MaterialTheme.colorScheme.error else denebHint()
    Row(
        Modifier
            .padding(end = 8.dp)
            .border(1.dp, tint, RoundedCornerShape(50))
            .padding(horizontal = 7.dp, vertical = 1.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text("${up.label.replace("검사", "")} ${ddayText(up.days)}", style = DenebType.meta, color = tint)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SiteMapLegend(sourcesPresent: List<String>, typesPresent: List<String>) {
    FlowRow(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainerLow, RoundedCornerShape(10.dp))
            .padding(horizontal = 12.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text("색=에너지원", style = DenebType.meta, color = denebHint())
        sourcesPresent.forEach { s ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(9.dp).background(sourceColor(s), RoundedCornerShape(50)))
                Spacer(Modifier.width(4.dp))
                Text(s, style = DenebType.meta)
            }
        }
        Text("모양=특성", style = DenebType.meta, color = denebHint())
        typesPresent.forEach { t ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Canvas(Modifier.size(11.dp)) {
                    drawMark(shapeOfType(t), Offset(size.width / 2, size.height / 2), 4.5.dp.toPx(), sourceEtc)
                }
                Spacer(Modifier.width(4.dp))
                Text(t, style = DenebType.meta)
            }
        }
        Text("크기=용량", style = DenebType.meta, color = denebHint())
    }
}

@Composable
private fun SiteMapCanvas(pins: List<SitePin>, selected: SitePin?, onPinTap: (SitePin) -> Unit) {
    val provinceStroke = MaterialTheme.colorScheme.outlineVariant
    val provinceFill = MaterialTheme.colorScheme.surface
    val hairline = denebHairline()
    // 핀 마크에 지도 배경색 링을 둘러 겹친 핀끼리, 그리고 지도면과 분리해 또렷하게 한다.
    val pinBorder = MaterialTheme.colorScheme.surfaceContainerLow
    // 핀을 눌렀을 때만 그 현장 이름을 지도 위 라벨로 띄운다 — 평소엔 지도를 비워 깔끔하게.
    // (시도 라벨을 상시 표시하지 않는다.) 라벨은 알약 배경 위 읽기 쉬운 텍스트.
    val labelText = MaterialTheme.colorScheme.onSurface
    val labelBg = MaterialTheme.colorScheme.surfaceContainerHighest
    val measurer = rememberTextMeasurer()
    val labelStyle = remember(labelText) { TextStyle(color = labelText, fontSize = 11.sp) }
    // Cache the parsed province paths once — PathParser is not cheap and the data is static.
    val provincePaths = remember { KoreaGeo.provinces.map { PathParser().parsePathString(it.d).toPath() } }

    // 핀치 줌 + 팬 (데스크톱 휠 줌 대응). scale/offset을 graphicsLayer로 적용한다. 탭
    // 좌표는 graphicsLayer의 역변환을 거쳐 detectTapGestures로 전달되므로, 아래 히트
    // 테스트는 확대 여부와 무관하게 원본(1×) 좌표계 그대로 쓰면 된다.
    var scale by remember { mutableStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }

    Box(
        Modifier
            .fillMaxWidth()
            .border(1.dp, hairline, RoundedCornerShape(14.dp))
            .background(MaterialTheme.colorScheme.surfaceContainerLow, RoundedCornerShape(14.dp))
            .padding(8.dp),
    ) {
        Canvas(
            Modifier
                .fillMaxWidth()
                .aspectRatio(KoreaGeo.WIDTH / KoreaGeo.HEIGHT)
                .graphicsLayer(scaleX = scale, scaleY = scale, translationX = offset.x, translationY = offset.y, clip = true)
                .pointerInput(Unit) {
                    // Engage zoom/pan only on a real pinch (2 fingers) or once already zoomed;
                    // a single-finger drag at 1× is left unconsumed so the parent list scrolls.
                    awaitEachGesture {
                        awaitFirstDown(requireUnconsumed = false)
                        do {
                            val event = awaitPointerEvent()
                            if (event.changes.count { it.pressed } >= 2 || scale > 1f) {
                                val zoom = event.calculateZoom()
                                val pan = event.calculatePan()
                                if (zoom != 1f || pan != Offset.Zero) {
                                    scale = (scale * zoom).coerceIn(1f, 5f)
                                    offset = if (scale > 1f) offset + pan else Offset.Zero
                                    event.changes.forEach { if (it.pressed) it.consume() }
                                }
                            }
                        } while (event.changes.any { it.pressed })
                    }
                }
                .pointerInput(pins) {
                    detectTapGestures { pos ->
                        val s = size.width / KoreaGeo.WIDTH
                        val threshold = 22.dp.toPx()
                        var best: SitePin? = null
                        var bestD = threshold
                        for (p in pins) {
                            val dx = pos.x - p.vx * s
                            val dy = pos.y - p.vy * s
                            val d = sqrt(dx * dx + dy * dy)
                            if (d <= bestD) {
                                bestD = d
                                best = p
                            }
                        }
                        best?.let(onPinTap)
                    }
                },
        ) {
            val s = size.width / KoreaGeo.WIDTH
            // Province outlines (scale the parsed paths into canvas space). Rounded joins
            // smooth the coastline; a hair thicker than before so it reads as a clean edge.
            val matrix = Matrix().apply { scale(s, s) }
            for (base in provincePaths) {
                val p = Path().apply { addPath(base) }
                p.transform(matrix)
                drawPath(p, provinceFill)
                drawPath(p, provinceStroke, style = Stroke(width = 1.2f, join = StrokeJoin.Round))
            }
            // Pins — halo + filled 특성 mark with a background-colored ring, sized by 용량,
            // colored by 에너지원.
            for (pin in pins) {
                val cx = pin.vx * s
                val cy = pin.vy * s
                val r = pin.radiusDp.dp.toPx()
                val color = sourceColor(pin.source)
                drawCircle(color, radius = r + 3.dp.toPx(), center = Offset(cx, cy), alpha = 0.16f)
                drawMark(shapeOfType(pin.type), Offset(cx, cy), r, color, pinBorder)
            }
            // 선택된 현장 이름 라벨 — 핀을 눌렀을 때만, 그 핀 옆에 알약 배경으로 (상시 아님).
            // Only when the selected pin is currently shown; measured on demand (single label).
            val sel = selected
            if (sel != null && pins.contains(sel)) {
                val name = sel.site.trim().substringAfterLast(' ').ifBlank { sel.project }
                val layout = measurer.measure(name, labelStyle)
                val padX = 6.dp.toPx()
                val padY = 3.dp.toPx()
                val lx = sel.vx * s + sel.radiusDp.dp.toPx() + 5.dp.toPx()
                val ly = sel.vy * s - layout.size.height / 2f
                drawRoundRect(
                    color = labelBg,
                    topLeft = Offset(lx - padX, ly - padY),
                    size = Size(layout.size.width + padX * 2, layout.size.height + padY * 2),
                    cornerRadius = CornerRadius(5.dp.toPx()),
                )
                drawText(layout, topLeft = Offset(lx, ly))
            }
        }
        // 맞춤 — reset zoom/pan, shown only while zoomed in (mirrors the desktop button).
        if (scale > 1f) {
            TextButton(
                onClick = {
                    scale = 1f
                    offset = Offset.Zero
                },
                modifier = Modifier.align(Alignment.TopEnd),
            ) { Text("맞춤", style = DenebType.button) }
        }
    }
}

// Draw a filled 특성 mark of radius r at [center]. circle=토지, square=루프탑,
// diamond=수상, triangle=기타. When [border] is specified, a thin ring is stroked
// around the fill so overlapping pins stay distinct against each other and the map.
private fun DrawScope.drawMark(shape: String, center: Offset, r: Float, color: Color, border: Color = Color.Unspecified) {
    val ring = if (border != Color.Unspecified) Stroke(width = 1.5f, join = StrokeJoin.Round) else null
    when (shape) {
        "square" -> {
            val tl = Offset(center.x - r, center.y - r)
            val sz = Size(r * 2, r * 2)
            drawRect(color, topLeft = tl, size = sz)
            if (ring != null) drawRect(border, topLeft = tl, size = sz, style = ring)
        }

        "triangle" -> {
            val p = Path().apply {
                moveTo(center.x, center.y - r * 1.15f)
                lineTo(center.x + r, center.y + r * 0.75f)
                lineTo(center.x - r, center.y + r * 0.75f)
                close()
            }
            drawPath(p, color)
            if (ring != null) drawPath(p, border, style = ring)
        }

        "diamond" -> {
            val p = Path().apply {
                moveTo(center.x, center.y - r * 1.2f)
                lineTo(center.x + r * 1.05f, center.y)
                lineTo(center.x, center.y + r * 1.2f)
                lineTo(center.x - r * 1.05f, center.y)
                close()
            }
            drawPath(p, color)
            if (ring != null) drawPath(p, border, style = ring)
        }

        else -> {
            drawCircle(color, radius = r, center = center)
            if (ring != null) drawCircle(border, radius = r, center = center, style = ring)
        }
    }
}
