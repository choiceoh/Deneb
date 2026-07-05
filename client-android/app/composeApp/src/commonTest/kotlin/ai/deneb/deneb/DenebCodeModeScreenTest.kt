package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals

class DenebCodeModeScreenTest {
    @Test
    fun codeTimeAgoUsesKoreanRelativeBuckets() {
        val now = 1_700_000_000_000L

        assertEquals("방금", codeTimeAgo("2023-11-14T22:13:10Z", now))
        assertEquals("5분 전", codeTimeAgo("2023-11-14T22:08:20Z", now))
        assertEquals("2시간 전", codeTimeAgo("2023-11-14T20:13:20Z", now))
        assertEquals("3일 전", codeTimeAgo("2023-11-11T22:13:20Z", now))
        assertEquals("2023-11-01", codeTimeAgo("2023-11-01T00:00:00Z", now))
        assertEquals("", codeTimeAgo("", now))
        assertEquals("", codeTimeAgo("not-a-date", now))
    }

    @Test
    fun codeSessionSubtitleIncludesFreshStatusAndCheckpointCount() {
        val session = CodeSession(
            repo = CodeRepo(owner = "choiceoh", name = "Deneb"),
            status = "passed",
            updatedAt = "2023-11-14T22:08:20Z",
            checkpoints = listOf(CodeCheckpoint(sha = "abc", summary = "수정", at = "2023-11-14T22:08:20Z")),
        )

        assertEquals(
            "choiceoh/Deneb · 검증 통과 · 5분 전 갱신 · 체크포인트 1",
            codeSessionSubtitle(session, nowEpochMs = 1_700_000_000_000L),
        )
    }

    @Test
    fun codeSessionSubtitleIncludesDirtyWorktreeCount() {
        val session = CodeSession(
            repo = CodeRepo(owner = "choiceoh", name = "Deneb"),
            status = "working",
            dirty = true,
            changedFiles = 3,
        )

        assertEquals("저장 필요 3개", codeDirtyLabel(session))
        assertEquals(
            "choiceoh/Deneb · 작업 중 · 저장 필요 3개",
            codeSessionSubtitle(session, nowEpochMs = 1_700_000_000_000L),
        )
    }
}
