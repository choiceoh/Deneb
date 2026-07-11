package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ToolStatusLabelsTest {

    private val expected = linkedMapOf(
        "calendar" to "일정 확인 중",
        "clarify" to "질문 정리 중",
        "contacts" to "연락처 확인 중",
        "cron" to "예약 작업 처리 중",
        "edit" to "파일 수정 중",
        "exec" to "명령 실행 중",
        "fetch_tools" to "도구 준비 중",
        "gateway" to "게이트웨이 점검 중",
        "gmail" to "메일 확인 중",
        "graphify" to "지식 그래프 작업 중",
        "grep" to "자료 검색 중",
        "heartbeat_update" to "상태 메모 갱신 중",
        "knowledge" to "지식 검색 중",
        "message" to "메시지 전송 중",
        "morning_letter" to "아침 편지 작성 중",
        "observe" to "시스템 점검 중",
        "phone_read" to "휴대폰 확인 중",
        "phone_write" to "휴대폰 제어 중",
        "polaris" to "컨텍스트 정리 중",
        "process" to "작업 프로세스 확인 중",
        "read" to "파일 확인 중",
        "read_spillover" to "추가 출력 확인 중",
        "send_file" to "파일 전송 중",
        "sessions" to "세션 확인 중",
        "sessions_spawn" to "보조 세션 시작 중",
        "skills" to "스킬 확인 중",
        "subagents" to "하위 작업 진행 중",
        "watch" to "감시 작업 설정 중",
        "web" to "웹 검색 중",
        "wiki" to "기억 검색 중",
        "write" to "파일 작성 중",
    )

    @Test
    fun constantsDescribeNonToolThinkingPhases() {
        assertEquals("깊이 생각 중…", ToolStatusLabels.THINKING)
        assertEquals("결과 검토 중…", ToolStatusLabels.REVIEWING)
    }

    @Test
    fun everyGatewayToolHasItsExpectedKoreanLabel() {
        expected.forEach { (tool, label) ->
            assertEquals(label, ToolStatusLabels.label(tool), tool)
        }
    }

    @Test
    fun failureLabelsReplaceTheProgressSuffix() {
        expected.forEach { (tool, label) ->
            assertTrue(label.endsWith(" 중"), tool)
            assertEquals(label.removeSuffix(" 중") + " 실패", ToolStatusLabels.failureLabel(tool), tool)
        }
    }

    @Test
    fun trailLabelsRemoveOnlyTheProgressSuffix() {
        expected.forEach { (tool, label) ->
            assertEquals(label.removeSuffix(" 중"), ToolStatusLabels.trailLabel(tool), tool)
        }
    }

    @Test
    fun unknownToolsRemainVisibleInsteadOfDisappearing() {
        assertEquals("future_tool", ToolStatusLabels.label("future_tool"))
        assertEquals("future_tool 실패", ToolStatusLabels.failureLabel("future_tool"))
        assertEquals("future_tool", ToolStatusLabels.trailLabel("future_tool"))
    }

    @Test
    fun toolLookupIsIntentionallyExactAndCaseSensitive() {
        assertEquals("GMAIL", ToolStatusLabels.label("GMAIL"))
        assertEquals(" gmail ", ToolStatusLabels.label(" gmail "))
        assertEquals(" 실패", ToolStatusLabels.failureLabel(""))
    }
}
