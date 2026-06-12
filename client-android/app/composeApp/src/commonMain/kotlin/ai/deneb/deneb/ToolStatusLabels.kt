package ai.deneb.deneb

/**
 * Korean status labels for gateway tool names, shown in the chat waiting chip
 * while a tool runs ("메일 확인 중" instead of the raw `gmail`). The map keys
 * mirror the gateway's tool registry (gateway-go/internal/pipeline/chat/toolreg/
 * tool_schemas.json); unknown tools fall back to their raw name so a new
 * gateway tool degrades to "● new_tool" rather than hiding.
 *
 * Korean-first by project principle — these are narration strings for the
 * single-operator deployment, not localized resources.
 */
internal object ToolStatusLabels {
    /** Waiting-chip label while the model streams reasoning (no tool yet). */
    const val THINKING = "깊이 생각 중…"

    /**
     * Waiting-chip label for the stretch between agent steps: the last running
     * tool finished and the model is back in an LLM step reading its results
     * (a cache-missed prefill can hold this silent for tens of seconds).
     */
    const val REVIEWING = "결과 검토 중…"

    // Noun + "~ 중" forms only: failureLabel swaps the " 중" suffix for
    // " 실패", so verb forms ("보내는 중") would conjugate badly there.
    private val labels = mapOf(
        "calendar" to "일정 확인 중",
        "clarify" to "질문 정리 중",
        "contacts" to "연락처 확인 중",
        "cron" to "예약 작업 처리 중",
        "dropbox" to "Dropbox 확인 중",
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

    fun label(tool: String): String = labels[tool] ?: tool

    /** Failure form for the chip ("메일 확인 중" → "메일 확인 실패"). */
    fun failureLabel(tool: String): String {
        val base = labels[tool] ?: return "$tool 실패"
        return if (base.endsWith(" 중")) base.removeSuffix(" 중") + " 실패" else "$base 실패"
    }

    /** Compact footprint form for the post-turn trail ("메일 확인 중" → "메일 확인"). */
    fun trailLabel(tool: String): String = (labels[tool] ?: return tool).removeSuffix(" 중")
}
