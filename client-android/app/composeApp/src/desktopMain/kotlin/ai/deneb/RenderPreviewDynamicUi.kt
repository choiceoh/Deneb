@file:OptIn(androidx.compose.ui.ExperimentalComposeUiApi::class)

package ai.deneb

import ai.deneb.deneb.FilesSearchMode
import ai.deneb.deneb.FilesSearchModeRow
import ai.deneb.deneb.IntervalUnit
import ai.deneb.deneb.SchedMode
import ai.deneb.deneb.ScheduleDraft
import ai.deneb.ui.DenebGroup
import ai.deneb.ui.DenebListRow
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.composables.DenebBottomBar
import ai.deneb.ui.denebInsight
import ai.deneb.ui.denebInsightContainer
import ai.deneb.ui.dynamicui.ChartNode
import ai.deneb.ui.dynamicui.DenebUiHtml
import ai.deneb.ui.dynamicui.DenebUiNode
import ai.deneb.ui.dynamicui.DenebUiRenderer
import ai.deneb.ui.dynamicui.LocalDenebUiMotion
import ai.deneb.ui.icons.outlined.AutoAwesome
import ai.deneb.ui.icons.outlined.Dns
import ai.deneb.ui.icons.outlined.Extension
import ai.deneb.ui.icons.outlined.Memory
import ai.deneb.ui.icons.outlined.Palette
import ai.deneb.ui.icons.outlined.Schedule
import ai.deneb.ui.icons.outlined.Visibility
import ai.deneb.ui.markdown.MarkdownContent
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
import androidx.compose.material.icons.Icons
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.ImageComposeScene
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.persistentListOf
import kotlinx.datetime.LocalDate
import org.jetbrains.skia.EncodedImageFormat
import java.io.File

// Render a registry screen to a PNG at [width]x[height]@[density] — the common
// ImageComposeScene plumbing the per-screen render* functions used to repeat.
internal fun renderScreen(pngName: String, screen: String, scheme: ColorScheme, width: Int, height: Int, density: Float = 2f) {
    val body = previewScreens[screen] ?: error("unknown preview screen '$screen'")
    val scene = ImageComposeScene(width = width, height = height, density = Density(density)) { body(scheme) }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$pngName").writeBytes(data.bytes)
    scene.close()
}

// Sample drafts for the cron edit previews — one per schedule mode so the segmented
// control, weekday chips, interval row, and raw-cron fallback all get exercised.
internal val cronWeeklyDraft = ScheduleDraft(SchedMode.WEEKLY, "08:00", setOf(1, 3, 5), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
internal val cronIntervalDraft = ScheduleDraft(SchedMode.INTERVAL, "09:00", emptySet(), "15", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "")
internal val cronAdvancedDraft = ScheduleDraft(SchedMode.ADVANCED, "09:00", emptySet(), "30", IntervalUnit.MIN, LocalDate.parse("2026-06-13"), "*/5 8-22 * * 1-6")

internal fun renderBottomBar(name: String, scheme: ColorScheme, route: String) {
    // Phone width (412dp = 824px @ density 2) so the bar matches the real device. The
    // navigate-action callbacks are no-ops here — this checks the icons/labels/selection
    // only (피드·메일·채팅·달력·더보기).
    val scene = ImageComposeScene(width = 824, height = 240, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.fillMaxSize()) {
                    Spacer(Modifier.weight(1f))
                    DenebBottomBar(
                        currentRoute = route,
                        onNavigate = {},
                    )
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

// Design-refresh pilot (2026-06): the grouped-inset card idiom + the two-accent
// system — cool primary on the selected row, warm apricot on the AI-insight callout.
internal fun renderDesignRefresh(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 824, height = 1380, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.fillMaxSize().padding(top = 26.dp)) {
                    Text(
                        "설정",
                        style = DenebType.viewTitle,
                        color = MaterialTheme.colorScheme.onBackground,
                        modifier = Modifier.padding(start = 24.dp, bottom = 16.dp),
                    )
                    DenebGroup(label = "시스템") {
                        DenebListRow("게이트웨이", {}, icon = Icons.Outlined.Dns, subtitle = "연결 · 버전 · 동기화")
                        DenebListRow("화면", {}, icon = Icons.Outlined.Palette, subtitle = "테마 · UI 배율")
                        DenebListRow("모델", {}, icon = Icons.Outlined.Memory, subtitle = "역할별 지정 · 엔드포인트", selected = true, divider = false)
                    }
                    Spacer(Modifier.height(22.dp))
                    DenebGroup(label = "자동화 · 관찰") {
                        DenebListRow("스킬", {}, icon = Icons.Outlined.Extension, subtitle = "설치 · Propus")
                        DenebListRow("크론", {}, icon = Icons.Outlined.Schedule, subtitle = "예약 작업")
                        DenebListRow("관찰", {}, icon = Icons.Outlined.Visibility, subtitle = "동작 · 로그", divider = false)
                    }
                    Spacer(Modifier.height(26.dp))
                    Row(
                        Modifier
                            .padding(horizontal = 16.dp)
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(16.dp))
                            .background(denebInsightContainer())
                            .padding(16.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        Icon(Icons.Outlined.AutoAwesome, contentDescription = null, tint = denebInsight(), modifier = Modifier.size(22.dp))
                        Spacer(Modifier.width(12.dp))
                        Column {
                            Text("AI 분석", style = DenebType.rowTitleStrong, color = denebInsight())
                            Spacer(Modifier.height(2.dp))
                            Text(
                                "탑솔라 견적 3건이 환차익 구간에 들어왔습니다. 월요일 콜 권장.",
                                style = DenebType.rowSubtitle,
                                color = MaterialTheme.colorScheme.onBackground,
                            )
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

private val mdTableSample = """
### 마크다운 표 (챗 파이프라인)

| 현장 | 규격 | 수량 | 납기 |
|---|---|---|---|
| 화성산단 RPS 2개소 | 100kW 계량반 | 12 | 7/10 |
| 부산 썬탑 7호 | 200kW 저압반 | 4 | 7/14 |
| 금호타이어 곡성화학공장 | 연계판넬 | 7 | 7/21 |

| # | 기능 | 핵심 |
|---|---|---|
| 1 | 코워킹 스페이스 | 이메일·캘린더·웹 접근 권한으로 전체 워크플로우 자동 수행 |
| 2 | 클라우드 실행 | 기기를 닫아도 작업 지속. 폰에서 실시간 모니터링 |
| 3 | 예약 작업 | 실행 시간 지정. 예: 매주 일요 밤 미답 메일 정리 |
"""

internal fun renderTableAB(name: String, scheme: ColorScheme) {
    val uiNode = parseLetterHtml(
        """
        <column>
          <card>
            <text style="title">deneb-ui table 노드</text>
            <table>
              <tr><th>현장</th><th>규격</th><th>수량</th><th>납기</th></tr>
              <tr><td>화성산단 RPS 2개소</td><td>100kW 계량반</td><td>12</td><td>7/10</td></tr>
              <tr><td>부산 썬탑 7호</td><td>200kW 저압반</td><td>4</td><td>7/14</td></tr>
              <tr><td>금호타이어 곡성화학공장</td><td>연계판넬</td><td>7</td><td>7/21</td></tr>
            </table>
            <table>
              <tr><th>#</th><th>기능</th><th>핵심</th></tr>
              <tr><td>1</td><td>코워킹 스페이스</td><td>이메일·캘린더·웹 접근 권한으로 자동 수행</td></tr>
              <tr><td>2</td><td>클라우드 실행</td><td>기기를 닫아도 작업 지속</td></tr>
              <tr><td>3</td><td>예약 작업</td><td>매주 일요 밤 미답 메일 정리</td></tr>
            </table>
          </card>
        </column>
        """.trimIndent(),
    )
    val scene = ImageComposeScene(width = 824, height = 1900, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.width(412.dp).padding(16.dp)) {
                    MarkdownContent(mdTableSample, Modifier, baseStyle = MaterialTheme.typography.bodyMedium)
                    Spacer(Modifier.height(20.dp))
                    CompositionLocalProvider(LocalDenebUiMotion provides false) {
                        DenebUiRenderer(node = uiNode, isInteractive = false, onCallback = { _, _ -> }, wrapInCard = false)
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

internal fun renderMarkdown(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 700, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                MarkdownContent(markdownSample, Modifier.padding(20.dp), baseStyle = MaterialTheme.typography.bodyMedium)
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// Validates the 이름 / 내용 / 의미 search-scope selector (FilesSearchModeRow): the
// Material SingleChoiceSegmentedButton with the given mode highlighted, at phone
// width — confirms the three Korean labels fit and the selected segment reads.
internal fun renderFilesSearchMode(name: String, scheme: ColorScheme, mode: FilesSearchMode) {
    val scene = ImageComposeScene(width = 824, height = 140, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                FilesSearchModeRow(mode = mode, onModeChange = {})
            }
        }
    }
    val image = scene.render()
    val data = image.encodeToData(EncodedImageFormat.PNG) ?: error("PNG encode failed")
    File("/tmp/deneb-render").mkdirs()
    File("/tmp/deneb-render/$name").writeBytes(data.bytes)
    scene.close()
}

// Reproduces the work-feed analysis answer (long prose paragraph + 2-col 라벨|내용
// table) at exactly phone width (412dp = 824px @ density 2). If the prose/table
// clip here, the bug is in the markdown component itself; if they wrap cleanly,
// the clip is the native-app window/LazyColumn measurement (Android is fine).
internal fun renderAnalysis(name: String, scheme: ColorScheme) {
    val analysisSample = """
        ## 사람과 조직
        | 구분 | 내용 |
        |:---|:---|
        | **발신** | 탑솔라 고건 대리(기획조정실) — 이전에도 대한전선 2차 사업 물량산출 자료를 동일 수신자에게 발송한 바 있음 |
        | **수신** | gocharge89@taihan.com — 태한(태양광 EPC 협력사) 담당자. 동일인의 다른 계정인지 불명 |

        **신호**: CC에 오선택 전무(남도에코에너지 대표 겸직)가 포함된 것은 통상적이나, 김대희·김유영은 이전 5/12 물량산출 메일에서도 CC였던 동일 인물. 에스컬레이션이나 담당자 교체 징후는 없다.
    """.trimIndent()
    val scene = ImageComposeScene(width = 824, height = 1400, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    MarkdownContent(analysisSample, modifier = Modifier.fillMaxWidth().padding(16.dp))
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

// The exact transcript shape the gateway's denebui.CollapsedReportFence emits
// for a per-mail analysis: an accordion (title-only collapsed card) wrapping a
// markdown body. expanded=true previews the post-tap state so the markdown
// child's rendering (headings/list/table) is visually checked too.
private fun collapsedReportFence(expanded: Boolean): String {
    // Body starts after the title line — the gateway strips the heading that
    // became the accordion title (collapsedReportBody) so the expanded card
    // doesn't open by repeating its own header.
    val body = "**발신**: fred@jocacable.com — 견적 회신 요청\\n\\n### 왜 지금 왔는가\\n- 5/12 물량산출 메일의 후속, 단가 협상 단계 진입\\n- **회신 기한: 6/13(금)** 명시\\n\\n| 구분 | 내용 |\\n|:---|:---|\\n| 거래처 | JOCA Cable (케이블 협력사) |\\n| 요청 | 1,950매 모듈 물량 견적 회신 |\\n\\n### 권고\\n1. 김민준 부장에게 단가표 확인 요청\\n2. 금요일 오전까지 회신 초안 준비"
    val exp = if (expanded) "\"expanded\":true," else ""
    return "```deneb-ui\n{\"type\":\"accordion\",\"title\":\"📧 JOCA Cable 최신 메일 분석 보고\",$exp\"children\":[{\"type\":\"markdown\",\"value\":\"$body\"}]}\n```"
}

internal fun renderCollapsedReport(name: String, scheme: ColorScheme, expanded: Boolean) {
    val scene = ImageComposeScene(width = 824, height = if (expanded) 1560 else 320, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    MarkdownContent(collapsedReportFence(expanded), modifier = Modifier.fillMaxWidth().padding(16.dp))
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

internal fun renderChart(name: String, scheme: ColorScheme) {
    val scene = ImageComposeScene(width = 840, height = 1000, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.padding(20.dp)) {
                    DenebUiRenderer(
                        node = ChartNode(
                            chartType = "bar",
                            labels = persistentListOf("1월", "2월", "3월", "4월"),
                            values = persistentListOf(12f, 28f, 19f, 34f),
                            label = "월별 매출",
                        ),
                        isInteractive = false,
                        onCallback = { _, _ -> },
                        wrapInCard = false,
                    )
                    Spacer(Modifier.height(24.dp))
                    DenebUiRenderer(
                        node = ChartNode(
                            chartType = "line",
                            labels = persistentListOf("월", "화", "수", "목", "금"),
                            values = persistentListOf(5f, 15f, 9f, 22f, 14f),
                            label = "주간 추세",
                        ),
                        isInteractive = false,
                        onCallback = { _, _ -> },
                        wrapInCard = false,
                    )
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

// Renders a morning/evening letter card (the deneb-ui node tree the letter
// skills now emit) at phone width so the card layout — stat strips, list rows,
// the D-N badge, section icons — can be eyeballed in dark and light before the
// SKILL.md templates ship. Mirrors skills/productivity/{morning,evening}-letter.
internal fun renderLetterCard(name: String, scheme: ColorScheme, node: DenebUiNode, height: Int) {
    val scene = ImageComposeScene(width = 824, height = height, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Column(Modifier.width(412.dp).padding(16.dp)) {
                    CompositionLocalProvider(LocalDenebUiMotion provides false) {
                        // wrapInCard=true = the CHAT call site's shape, so these
                        // previews pin what the phone actually shows mid-chat
                        // (a chromed root must not gain a second outer card).
                        DenebUiRenderer(
                            node = node,
                            isInteractive = false,
                            onCallback = { _, _ -> },
                        )
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

// Weather uses a headline temp + caption line (not three stats abreast) and FX
// is a 2-up stat row + full-width copper — tuned for phone width.
// Letter-card previews parse the CANONICAL HTML skeletons — the same markup
// skills/productivity/morning-letter/SKILL.md and the evening_letter tool
// contract (toolreg/core.go) instruct the model to emit. The PNGs therefore
// show exactly what the phone renders for a contract-conformant letter, and a
// parser regression breaks the preview loudly instead of drifting silently.
private fun parseLetterHtml(html: String): DenebUiNode = DenebUiHtml.parse(html) ?: error("letter skeleton failed to parse")

// Deliberate divergence from the SKILL.md skeleton on the market stats: the
// skeleton's values are digit-free letter tokens ("{{market:usd_krw}}") the
// gateway substitutes at delivery; this preview renders the POST-substitution
// form the user actually sees.
internal fun morningLetterNode(): DenebUiNode = parseLetterHtml(
    """
    <column>
      <text style="headline">7월 7일 화요일</text>
      <text style="caption">아침 레터 · 데네브</text>
      <hr/>
      <card>
        <row><icon name="sunny" size="16"/><text style="caption">날씨 · 광주</text></row>
        <row><text style="headline">18°</text><text style="caption">체감 16°</text></row>
        <text style="caption">최고 24° · 최저 14° · 강수 30%</text>
        <text style="body">오후 소나기 가능 — 우산 챙기세요</text>
      </card>
      <card>
        <row><icon name="payments" size="16"/><text style="caption">환율 · 구리</text></row>
        <row><stat value="1,386" label="USD/KRW"/><stat value="${'$'}9,540 /t" label="LME 구리"/></row>
      </card>
      <card>
        <row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row>
        <ul><li>09:00 — 팀 스탠드업</li><li>14:00 — 거래처 미팅</li></ul>
      </card>
      <card>
        <row><icon name="mail" size="16"/><text style="caption">전일 메일</text></row>
        <ul><li>김부장 — 견적서 회신 요청</li><li>세무서 — 부가세 신고 안내</li></ul>
      </card>
      <card>
        <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
        <row><text style="body">부가세 신고</text><badge color="warning">D-2</badge></row>
        <row><text style="body">진코 선입금 상계</text><badge color="error">기한 초과</badge></row>
      </card>
    </column>
    """.trimIndent(),
)

// Worst-case gallery: every read-only node + representative interactive ones,
// dense Korean business content (long labels, 4-column table, 5-point charts).
// Mirrors what a thorough agent answer SHOULD look like — when this looks
// bland, the renderer (not the grammar) is what needs work.
internal fun uiGalleryNode(): DenebUiNode = parseLetterHtml(
    """
    <column>
      <card>
        <text style="headline">3분기 발전소 현황</text>
        <text style="caption">2026-07-07 · 주간 보고 자동 생성</text>
        <hr/>
        <row><stat value="381톤" label="주간 철골 생산"/><stat value="68%" label="고흥해밀 공정"/><stat value="2.4억" label="미수금"/></row>
        <progress value="0.68" label="전체 공정률"/>
      </card>
      <card>
        <text style="title">현장별 공정률</text>
        <chart type="bar" label="공정률(%)">
          <point label="석문호" value="50"/><point label="고흥해밀" value="68"/><point label="비금도" value="35"/><point label="영광" value="82"/><point label="당진" value="12"/>
        </chart>
        <chart type="line" label="주간 생산량(톤)">
          <point label="6/9" value="290"/><point label="6/16" value="310"/><point label="6/23" value="275"/><point label="6/30" value="360"/><point label="7/7" value="381"/>
        </chart>
        <chart type="line" label="월간 손익(억)">
          <point label="3월" value="4"/><point label="4월" value="-3"/><point label="5월" value="2"/><point label="6월" value="6"/>
        </chart>
      </card>
      <card>
        <text style="title">수배전반 납품 일정</text>
        <table>
          <tr><th>현장</th><th>규격</th><th>수량</th><th>납기</th></tr>
          <tr><td>**화성산단** RPS 2개소</td><td>100kW 계량반</td><td>**12**</td><td>7/10</td></tr>
          <tr><td>부산 썬탑 7호</td><td>200kW 저압반</td><td>4</td><td>7/14</td></tr>
          <tr><td>금호타이어 곡성화학공장</td><td>연계판넬</td><td>7</td><td>7/21</td></tr>
        </table>
      </card>
      <card>
        <row><badge color="success">완료</badge><badge color="warning">지연 위험</badge><badge color="error">중단</badge><badge>기본</badge></row>
        <alert severity="warning" title="영광 도오리 민원">**약 3일 지연** 예상 — 작업 재개 협의 중.</alert>
        <alert severity="info" title="장마 대비">전 현장 24시간 비상대응체계 재확인 완료.</alert>
        <row><icon name="check_circle" color="success" size="16"/><text color="success">**전 현장 정상 가동** — 금주 안전사고 0건</text></row>
        <row><icon name="warning" color="warning" size="16"/><text color="warning">비금도 자재 입고 지연 — 월요일 재확인</text></row>
        <code language="sql">SELECT site, SUM(tons) AS total
FROM weekly_output GROUP BY site
ORDER BY total DESC;</code>
        <blockquote source="주간회의">발전소는 **가성비와 품질** 기준을 지켜 짓는다.</blockquote>
        <!-- Inverted slider range (min>max): guards against the coerceIn
             crash — this line would blank the whole render before the fix. -->
        <slider id="gallery_slider" label="가중치" min="100" max="0" value="50"/>
      </card>
      <card>
        <tabs selected-index="0">
          <tab label="결정사항"><ul><li>하계휴가 7/15부터 시행</li><li>임원 일정은 회장님 제출 후 확정</li><li>**고건** — 당진 구조검토 회신 (모델이 키를 직접 볼드한 케이스)</li></ul></tab>
          <tab label="액션 아이템"><ul><li>전 현장 비상대응체계 재확인</li></ul></tab>
        </tabs>
        <accordion title="회의 전문 발췌">
          <text style="body">보고드리겠습니다. 지난주에 정보보안 준수 서약을 했음에도 정보 유출이 의심되는 일이 있었습니다.</text>
        </accordion>
        <row><button variant="filled">회의록 열기</button><button variant="tonal">공유</button><button variant="outlined">재분석</button></row>
      </card>
    </column>
    """.trimIndent(),
)

// Corpus audit: DENEB_RENDER_CARDS_DIR renders every *.txt fence body in that
// directory (deneb-ui cards mined from real transcripts, kept OUT of the repo)
// at phone width, dark scheme — the loop for "how do PRODUCTION cards actually
// look", not just the curated gallery. Unparseable bodies (legacy JSON relics)
// are skipped. No-op when the env var is absent, so the standard preview run
// is unaffected.
internal fun renderCardCorpus() {
    val dir = System.getenv("DENEB_RENDER_CARDS_DIR")?.takeIf { it.isNotBlank() } ?: return
    val files = File(dir).listFiles { f -> f.extension == "txt" }?.sortedBy { it.name } ?: return
    var rendered = 0
    for (f in files) {
        val node = DenebUiHtml.parse(f.readText().trim()) ?: continue
        // Height scales with source size so long briefings aren't clipped;
        // the PNG is white below the content, which is fine for an audit.
        val height = (600 + f.length().toInt() * 2 / 5).coerceIn(800, 6000)
        renderLetterCard("corpus_${f.nameWithoutExtension}.png", ai.deneb.ui.OledColorScheme, node, height)
        rendered++
    }
    println("card corpus rendered: $rendered/${files.size} -> /tmp/deneb-render/corpus_*.png")
}

// Message-corpus audit: DENEB_RENDER_MSGS_DIR renders each *.txt as a FULL
// assistant message through MarkdownContent (prose blocks + any embedded
// fences) at phone width — the vertical-rhythm loop over real replies, where
// the card-corpus var above audits fence bodies alone. Same rules: data stays
// out of the repo; absent env var = no-op.
internal fun renderMessageCorpus() {
    val dir = System.getenv("DENEB_RENDER_MSGS_DIR")?.takeIf { it.isNotBlank() } ?: return
    val files = File(dir).listFiles { f -> f.extension == "txt" }?.sortedBy { it.name } ?: return
    var rendered = 0
    for (f in files) {
        val text = f.readText().trim()
        if (text.isEmpty()) continue
        val height = (400 + f.length().toInt() / 2).coerceIn(800, 6000)
        renderMessageDoc("msg_${f.nameWithoutExtension}.png", ai.deneb.ui.OledColorScheme, text, height)
        rendered++
    }
    println("message corpus rendered: $rendered/${files.size} -> /tmp/deneb-render/msg_*.png")
}

private fun renderMessageDoc(name: String, scheme: ColorScheme, text: String, height: Int) {
    // Pre-parse like the chat path (document overload): the string overload
    // parses >2K bodies async on a background core, and this scene captures a
    // single frame — a long reply would snapshot blank mid-parse.
    val doc = ai.deneb.ui.markdown.parseMarkdown(text)
    val scene = ImageComposeScene(width = 824, height = height, density = Density(2f)) {
        MaterialTheme(colorScheme = scheme) {
            Surface(color = MaterialTheme.colorScheme.background) {
                Box(Modifier.width(412.dp)) {
                    CompositionLocalProvider(LocalDenebUiMotion provides false) {
                        MarkdownContent(document = doc, modifier = Modifier.fillMaxWidth().padding(16.dp))
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

internal fun eveningLetterNode(): DenebUiNode = parseLetterHtml(
    """
    <column>
      <card>
        <row><icon name="calendar" size="16"/><text style="caption">내일 일정</text></row>
        <ul><li>10:00 — 분기 리뷰</li><li>15:00 — 거래처 콜</li></ul>
      </card>
      <card>
        <row><icon name="mail" size="16"/><text style="caption">챙길 메일</text></row>
        <ul><li>이대리 — 내일 회의자료 공유</li></ul>
      </card>
      <card>
        <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
        <row><text style="body">부가세 신고</text><badge color="warning">D-2</badge></row>
      </card>
    </column>
    """.trimIndent(),
)
