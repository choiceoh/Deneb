package ai.deneb.ui.chat

import kotlin.test.Test
import kotlin.test.assertEquals

class WorkFeedLogCardTest {
    @Test
    fun classifiesSystemCardsAsLogAndWorkCardsAsFeed() {
        val cases = listOf(
            Triple("genesis-meta", "메타 개정 제안: evolve-system-prompt.md", true),
            Triple("genesis-ladder", "졸업 사다리", true),
            Triple("system_log", "무엇이든", true),
            Triple("proactive", "📊 모델 튜너: 최근 24시간 분석", true),
            Triple("proactive", "self-correction 후보 8건 상태 분석", true),
            Triple("proactive", "자가개선 후보 1건 — 결정이 필요해요", true),
            Triple("proactive", "GLM-5.2 토큰·지연 급증 및 K3 오류 상승", true),
            Triple("proactive", "건창 인버터 결재 58시간 지연", false),
            Triple("mail_report", "JOCA 견적 회신", false),
            Triple("groupware-approval", "모듈대금 지출건", false),
            Triple("proactive", "임원 출근일 안내", false),
        )
        for ((source, title, want) in cases) {
            assertEquals(want, isWorkFeedLogCard(source, title), "$source / $title")
        }
    }
}
