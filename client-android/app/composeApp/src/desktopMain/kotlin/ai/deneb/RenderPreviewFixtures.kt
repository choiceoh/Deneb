@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

import ai.deneb.deneb.generated.DashboardItem
import ai.deneb.deneb.generated.LaneOut
import ai.deneb.deneb.generated.MemberOut
import ai.deneb.deneb.generated.OrgNodeOut
import ai.deneb.deneb.generated.RSIHealthView
import ai.deneb.deneb.generated.RSILayerView
import ai.deneb.deneb.generated.RSILoopStatusResponse
import ai.deneb.deneb.generated.RSIMetricView
import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SelfImprovementCodingFunnel
import ai.deneb.deneb.generated.SelfImprovementCodingListResponse
import ai.deneb.deneb.generated.SelfImprovementCodingStatusCount
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.components.SkeletonList
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.collections.immutable.persistentListOf
import org.jetbrains.skia.EncodedImageFormat
import java.io.File

// --- Home widget mirror ---
// The Android home widget is RemoteViews (androidApp/deneb_widget.xml +
// DenebWidgetProvider.render). Paparazzi's layoutlib has no Linux-aarch64 native
// binary, so it can't render on this host; instead we reproduce the widget's
// exact layout, colors, and Material glyph paths in Compose/Skia. Keep these in
// sync with deneb_widget.xml whenever the widget changes.
private const val WIDGET_CAL_PATH =
    "M19,3h-1V1h-2v2H8V1H6v2H5C3.89,3 3,3.9 3,5v14c0,1.1 0.89,2 2,2h14c1.1,0 2,-0.9 2,-2V5C21,3.9 20.1,3 19,3zM19,19H5V8h14V19z"
private const val WIDGET_MAIL_PATH =
    "M20,4H4c-1.1,0 -1.99,0.9 -1.99,2L2,18c0,1.1 0.9,2 2,2h16c1.1,0 2,-0.9 2,-2V6c0,-1.1 -0.9,-2 -2,-2zM20,8l-8,5 -8,-5V6l8,5 8,-5v2z"

private fun widgetGlyph(pathData: String): ImageVector = ImageVector.Builder(
    defaultWidth = 24.dp,
    defaultHeight = 24.dp,
    viewportWidth = 24f,
    viewportHeight = 24f,
).apply {
    addPath(PathParser().parsePathString(pathData).toNodes(), fill = SolidColor(Color.White))
}.build()

internal fun renderWidget(name: String, meeting: String, latestMail: String, unread: String) {
    val homeBg = Color(0xFF0B0B12)
    val cardBg = Color(0xFF1A1B26)
    val accent = Color(0xFF7AA2F7)
    val titleColor = Color(0xFFE8EAF0)
    val mailColor = Color(0xFFD5D8E5)
    val subColor = Color(0xFFA9B1D6)
    val scene = ImageComposeScene(width = 640, height = 420, density = Density(2f)) {
        Box(Modifier.fillMaxSize().background(homeBg).padding(24.dp)) {
            Column(
                Modifier.fillMaxWidth()
                    .clip(RoundedCornerShape(20.dp))
                    .background(cardBg)
                    .padding(16.dp),
            ) {
                Text("Deneb", color = accent, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(10.dp))
                Row {
                    Icon(widgetGlyph(WIDGET_CAL_PATH), null, Modifier.padding(top = 2.dp).size(16.dp), tint = accent)
                    Spacer(Modifier.width(8.dp))
                    Text(
                        meeting,
                        color = titleColor,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.Bold,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                if (latestMail.isNotEmpty()) {
                    Spacer(Modifier.height(10.dp))
                    Row {
                        Icon(widgetGlyph(WIDGET_MAIL_PATH), null, Modifier.padding(top = 1.dp).size(14.dp), tint = subColor)
                        Spacer(Modifier.width(8.dp))
                        Column {
                            Text(
                                latestMail,
                                color = mailColor,
                                fontSize = 13.sp,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (unread.isNotEmpty()) {
                                Spacer(Modifier.height(1.dp))
                                Text(unread, color = subColor, fontSize = 11.sp)
                            }
                        }
                    }
                }
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// Validates the decluttered work-feed bottom sheet: clean rows (title + relative
// time + 2-line summary) with trailing icon-only quick actions instead of a wall
// of labeled buttons. Renders several mock items at phone width.
internal val sampleFeed = persistentListOf(
    WorkFeedItem(
        id = "wf1",
        source = "proactive",
        title = "📧 JOCA Cable 최신 메일 분석 보고",
        summary = "발신 fred@jocacable.com — 2800km solar cable 대량 발주 가격 제안, 발주 수량·시점 회신 요청.",
        clusterId = "wfc-cable-order",
        relatedIds = listOf("wf2"),
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "후속 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 15 * 60_000L,
    ),
    WorkFeedItem(
        id = "wf2",
        source = "proactive",
        title = "분석 — 왜 지금 왔는가",
        summary = "무림 울산공장 풍력 사업의 첫 검토안 제출 — 박종원 부장이 외부 업체 제안 자료를 전달.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "후속 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 40 * 60_000L,
    ),
    WorkFeedItem(
        id = "wf3",
        source = "capture_audio",
        title = "공유 녹음",
        summary = "기획조정실 주간 회의 — RE100 고객사 계약 진행률, 주차장 태양광 견적 리뷰를 논의했습니다.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "액션 정리"),
            WorkFeedAction("snooze", "snooze", "나중에"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 3 * 3_600_000L,
    ),
    WorkFeedItem(
        id = "wf4",
        source = "capture_image",
        title = "공유 이미지",
        summary = "현대차 울산 견적서 OCR — 합계 ₩2,800,000, 납기 6/20, 결제 조건 30일.",
        status = "unread",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("followup", "followup", "문서화"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 5 * 3_600_000L,
    ),
    WorkFeedItem(
        id = "wf5",
        source = "proactive",
        title = "📋 주간업무보고 — 기획조정실",
        summary = "이번 주 사업개발 3건·모듈 견적 5건 발송, 루프탑 2건 계약 임박.",
        status = "acked",
        actions = listOf(
            WorkFeedAction("open", "open", "열기"),
            WorkFeedAction("ack", "ack", "완료"),
        ),
        createdAtMs = System.currentTimeMillis() - 26 * 3_600_000L,
    ),
)

// The part-grouped work dashboard (파트별 업무 현황): the five fixed 파트 lanes (one
// empty to exercise the "지금 할 일이 없습니다" line) plus the muted 미분류 triage lane.
// Mixed scheduled times (오늘/내일/dated) exercise dashboardTimeLabel; lane 2 is empty.
internal val sampleDashboard = listOf(
    LaneOut(
        key = "team1",
        name = "기획조정실 1팀 (인허가)",
        items = listOf(
            DashboardItem("RE100 고객사 인허가 서류 제출", "본사 3층 · 김민준 부장", "calendar", "calendar", "e1", System.currentTimeMillis() + 2 * 3_600_000L),
            DashboardItem("남도에코 모듈 입고 점검", "현장 — 1,950매 검수", "calendar", "calendar", "e2", System.currentTimeMillis() + 26 * 3_600_000L),
        ),
    ),
    LaneOut(key = "team2", name = "기획조정실 2팀 (루프탑)", items = emptyList()),
    LaneOut(
        key = "team3",
        name = "기획조정실 3팀 (모듈)",
        items = listOf(
            DashboardItem("📧 JOCA Cable 견적 회신 요청", "발신 fred@jocacable.com — 회신 기한 6/13(금)", "mail_report", "workfeed", "wf1", System.currentTimeMillis() - 40 * 60_000L),
        ),
    ),
    LaneOut(
        key = "namdo",
        name = "남도에코에너지",
        items = listOf(
            DashboardItem("준공정산서 검토 — 남도에코", "₩19,500,000 · 결제 30일", "capture_image", "workfeed", "wf2", System.currentTimeMillis() + 3 * 3_600_000L),
        ),
    ),
    LaneOut(
        key = "personal",
        name = "개인 / 기타",
        items = listOf(
            DashboardItem("법인카드 정산 (5월분)", "마감 임박", "proactive", "workfeed", "wf3", 0L),
        ),
    ),
    LaneOut(
        key = "unclassified",
        name = "미분류",
        items = listOf(
            DashboardItem("무림 울산공장 풍력 검토안", "박종원 부장 — 담당 파트 미지정", "proactive", "workfeed", "wf4", System.currentTimeMillis() + 50 * 3_600_000L),
        ),
    ),
)

// Mirrors a real prod snapshot so the preview exercises every state badge color
// (LIVE / DATA-GATED / STARVED) and metric layout.
internal val sampleRsi = RSILoopStatusResponse(
    turning = 2,
    layers = listOf(
        RSILayerView(
            key = "L1",
            title = "스킬 진화",
            state = "LIVE",
            diagnosis = "이번 주 진화 3 · 신규 스킬 2 · 기각 5",
            detail = "저성과 스킬의 본문을 자동으로 다시 쓰고, 보류 검증과 롤백으로 회귀를 막는 기본 자가개선 루프입니다.",
            metrics = listOf(
                RSIMetricView("진화(7일)", "3"),
                RSIMetricView("신규 스킬", "2"),
                RSIMetricView("기각", "5"),
                RSIMetricView("확정률", "62%"),
            ),
        ),
        RSILayerView(
            key = "L2",
            title = "메타 진화",
            state = "LIVE",
            diagnosis = "이번 주 슬로우 루프 개정 1 · 제안 1 (최근: producer)",
            detail = "스킬을 고치는 프롬프트(생성·판정) 자체를 주간 단위로 개정하는 메타 루프입니다.",
            metrics = listOf(
                RSIMetricView("개정(7일)", "1"),
                RSIMetricView("제안", "1"),
                RSIMetricView("최근 에폭", "producer"),
            ),
        ),
        RSILayerView(
            key = "L3",
            title = "판정자 공진화",
            state = "DATA-GATED",
            diagnosis = "4회 실행; 판정자가 명백한 결함은 모두 잡았고 미묘 프로브는 아직 원장에 없습니다",
            detail = "판정자가 자신의 오판으로 학습하는 검증기 공진화 루프입니다.",
            metrics = listOf(
                RSIMetricView("실행(7일)", "4"),
                RSIMetricView("판정 놓침", "0"),
                RSIMetricView("오기각", "0"),
            ),
        ),
        RSILayerView(
            key = "L4",
            title = "소스 자가편집",
            state = "STARVED",
            diagnosis = "후보 13건(skill:9, test:4)이지만 배차 가능한 코드 후보가 아직 없습니다",
            detail = "게이트웨이 소스 자체를 고치는 자가편집 루프입니다. 근거 있는 후보만 코딩 레인에 배차됩니다.",
            metrics = listOf(
                RSIMetricView("후보", "13"),
                RSIMetricView("코드 후보", "0"),
                RSIMetricView("배차 가능", "0"),
            ),
        ),
    ),
    health = RSIHealthView(
        evolves7d = 3,
        confirmed7d = 2,
        rejected7d = 5,
        rolledBack7d = 1,
        genesis7d = 2,
        confirmRate = 0.67,
        falseAcceptRate = 0.33,
        resolvedEvolves7d = 3,
        thrash = false,
        autoAdoptFrozen = true,
        metaRevisions7d = 1,
    ),
)

// The org chart (조직도): a group → 실/회사 → 팀 → 파트 hierarchy joined by parentId,
// with lane-tagged parts (the 파트 chip) and members carrying 직급/직책. Fake names only
// (mirrors org.example.json) — exercises the multi-level DIAGRAM (boxes + connector
// lines), type badges, lane chips, leader/member-count summaries, a 4th level (the two
// sub-parts under 1팀) to show depth, a 겸직 (김철수 appears in both 기획조정실 and 1팀)
// for the search demo, and the bare 개인/기타 node (no members) for the empty-summary box.
// A few members carry FAKE phones/emails (as the gateway's GET enrichment would attach
// from the contacts store) so the contact call/email shortcuts render in the editor +
// search previews: 김철수 has both (search-result + editor), 이몽룡 email-only and 성춘향
// phone-only (single-glyph cases), the rest none (no contact row).
internal val sampleOrg = listOf(
    OrgNodeOut(id = "group", name = "예시그룹", type = "group", parentId = "", members = listOf(MemberOut("홍길동", "회장", "회장"))),
    OrgNodeOut(
        id = "planning",
        name = "기획조정실",
        type = "division",
        parentId = "group",
        members = listOf(MemberOut("김철수", "전무", "실장", phones = listOf("010-0000-0001"), emails = listOf("kim@example.test"))),
    ),
    OrgNodeOut(
        id = "team1",
        name = "기획조정실 1팀",
        type = "team",
        parentId = "planning",
        lane = "team1",
        members = listOf(
            MemberOut("김철수", "전무", "팀장", phones = listOf("010-0000-0001"), emails = listOf("kim@example.test")),
            MemberOut("이몽룡", "과장", "팀원", emails = listOf("lee@example.test")),
        ),
        keywords = listOf("인허가", "개발행위"),
        companies = listOf("사아건설"),
    ),
    // 4th level: two parts under 1팀 — exercises a deeper branch + the connector bus.
    OrgNodeOut(id = "team1a", name = "인허가파트", type = "team", parentId = "team1", lane = "team1a", keywords = listOf("인허가", "개발행위허가", "발전사업허가"), companies = listOf("한국전력", "산업부"), members = listOf(MemberOut("이몽룡", "과장", "팀장", emails = listOf("lee@example.test")))),
    OrgNodeOut(id = "team1b", name = "개발행위파트", type = "team", parentId = "team1", members = listOf(MemberOut("방자", "대리", "팀원"))),
    OrgNodeOut(
        id = "team2",
        name = "기획조정실 2팀",
        type = "team",
        parentId = "planning",
        lane = "team2",
        members = listOf(MemberOut("성춘향", "부장", "팀장", phones = listOf("010-0000-0002")), MemberOut("변학도", "대리", "팀원")),
        keywords = listOf("루프탑", "지붕"),
    ),
    OrgNodeOut(
        id = "namdo",
        name = "남도에코",
        type = "company",
        parentId = "group",
        lane = "namdo",
        members = listOf(MemberOut("장끼동", "상무", "대표"), MemberOut("까투리", "주임", "팀원")),
        keywords = listOf("케이블", "전선"),
        companies = listOf("가나에너지", "다라전기"),
    ),
    OrgNodeOut(id = "personal", name = "개인/기타", type = "team", parentId = "group"),
)

// Validates the loading skeleton (sweeping-shimmer placeholders). A static capture
// shows the base tint at rest; the highlight band only appears mid-sweep, so this
// mainly guards that the placeholder reads as visible (not a blank screen) and that
// the draw-phase shimmer doesn't crash.
internal fun renderSkeleton(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 700, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                SkeletonList(rows = 6)
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// --- Skills tab (settings) -------------------------------------------------
// Validates the origin badges (생성 vs 최초) on the skill list and the
// Propus timeline rows (genesis/evolved/rejected/review badges).

internal val sampleSkillRows = listOf(
    SkillRow(
        name = "email-analysis",
        description = "새 메일 도착(cron 트리거) 또는 직접 요청 시 Gmail 단일 메일을 심층 분석하는 워크플로우 — 스레드 수집, 위키 컨텍스트 결합, 중요도 분류.",
        category = "productivity",
        source = "managed",
        version = "1.1.1",
        origin = "initial",
        evolveCount = 2,
        totalUses = 7,
        tags = listOf("mail", "analysis"),
        relatedSkills = listOf("meeting-minutes"),
        dependencySummary = listOf("bins gh", "env GMAIL_TOKEN"),
        installSummary = listOf("Install GitHub CLI"),
    ),
    SkillRow(
        name = "morning-letter-composite",
        description = "아침 브리핑 편지를 일정·메일·할일 데이터로 합성하는 절차",
        category = "productivity", source = "managed", version = "0.1.0",
        origin = "genesis", createdAt = 1L, curatorState = "active", totalUses = 2,
    ),
    SkillRow(
        name = "playwright",
        description = "브라우저 자동화 작업 절차",
        category = "integration",
        source = "managed",
        version = "1.0.0",
        origin = "initial",
    ),
)

internal fun sampleLifecycleEvents(now: Long) = listOf(
    SkillLifecycleEvent(
        type = "evolved",
        skillName = "email-analysis",
        at = now - 2 * 3_600_000L,
        version = "1.1.1",
        detail = "gmail 도구 오류 시 가용 정보로 보고를 완료하도록 절차 보강",
    ),
    SkillLifecycleEvent(
        type = "review",
        skillName = "email-analysis",
        at = now - 3 * 3_600_000L,
        route = "no-op",
        detail = "기존 email-analysis 스킬이 해당 워크플로우를 이미 커버. 세션은 단일 메일 분석 요청으로 스킬 범위 내 — 새 스킬 생성이나 절차 변경 근거 없음.",
        evidence = "cron(email-single-analysis) → gmail 스레드 수집 → 위키 컨텍스트 결합 → 중요도 분류까지 기존 절차대로 완주",
    ),
    SkillLifecycleEvent(
        type = "evolve_rejected",
        skillName = "email-analysis",
        at = now - 26 * 3_600_000L,
        detail = "self-test rejected: 절차 퇴보 (위키 업데이트 단계 누락)",
    ),
    SkillLifecycleEvent(
        type = "genesis",
        skillName = "morning-letter-composite",
        at = now - 50 * 3_600_000L,
        detail = "아침 편지 합성 절차를 재사용 스킬로 추출",
    ),
)

internal fun sampleSelfImprovementCodingQueue(now: Long) = SelfImprovementCodingListResponse(
    candidates = listOf(
        SelfCorrectionCandidate(
            id = "sc-coding-1",
            status = "proposed",
            scope = "code",
            title = "도구 설명/스키마 품질: web (오류율 30%)",
            proposedChange = "web 도구의 ToolDef.Description을 다듬어 반복 오사용을 줄인다",
            evidence = "observe.behavior 30d: web calls=200 errors=60 (30%)",
            risk = "propose-only; 설명만 다듬고 권한 표면은 넓히지 않는다",
            source = "tool-quality:web:desc",
            autoDispatch = true,
            targetFiles = listOf("toolreg/core.go"),
            evidenceKinds = listOf("evidence", "target_files", "risk"),
            reviewActions = listOf("inspect_target_files", "run_focused_validation", "mark_review_status"),
            createdAt = now - 45 * 60_000L,
            updatedAt = now - 45 * 60_000L,
        ),
        SelfCorrectionCandidate(
            id = "sc-coding-2",
            status = "proposed",
            scope = "code",
            title = "죽은 코드: orphanHelper",
            proposedChange = "unreachable 함수를 삭제하거나 근거와 함께 베이스라인 처리",
            evidence = "deadcode-audit NEW: internal/pipeline/chat/run_orphan.go :: orphanHelper",
            source = "deadcode-finding:1a2b3c4d5e6f",
            autoDispatch = false,
            targetFiles = listOf("gateway-go/internal/pipeline/chat/run_orphan.go"),
            evidenceKinds = listOf("evidence", "target_files"),
            createdAt = now - 90 * 60_000L,
            updatedAt = now - 90 * 60_000L,
        ),
    ),
    count = 2,
    statusCounts = listOf(
        SelfImprovementCodingStatusCount(status = "proposed", count = 2),
        SelfImprovementCodingStatusCount(status = "accepted", count = 0),
        SelfImprovementCodingStatusCount(status = "applied", count = 1),
        SelfImprovementCodingStatusCount(status = "rejected", count = 1),
        SelfImprovementCodingStatusCount(status = "superseded", count = 0),
        SelfImprovementCodingStatusCount(status = "all", count = 4),
    ),
    funnel = SelfImprovementCodingFunnel(
        lastCaptureAt = now - 4 * 86_400_000L,
        lastReviewAt = now - 3 * 86_400_000L,
        rejections7d = 2,
        promotableRejections7d = 0,
        lastRejectionAt = now - 86_400_000L,
        lastNudgeAt = 0L,
    ),
)
