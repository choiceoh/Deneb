package ai.deneb.ui.chat

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotEquals

/**
 * The message list keys its LazyColumn on History.id with animateItem, so a
 * transcript message must keep a STABLE id across cache/network reloads — else
 * the re-key makes the old and new rows overlap ("두 번 렌더링돼 겹침"). These pin
 * that the id is a pure function of role + content + timestamp.
 */
class StableTranscriptIdTest {

    @Test
    fun sameMessageYieldsSameIdAcrossRebuilds() {
        val a = stableTranscriptId(History.Role.ASSISTANT, "안녕하세요 반갑습니다", 1_700_000_000_123L)
        val b = stableTranscriptId(History.Role.ASSISTANT, "안녕하세요 반갑습니다", 1_700_000_000_123L)
        assertEquals(a, b, "동일 (role, content, ts)는 재구성마다 같은 id여야 한다")
    }

    @Test
    fun differsByRoleContentAndTimestamp() {
        val base = stableTranscriptId(History.Role.USER, "네", 100L)
        assertNotEquals(base, stableTranscriptId(History.Role.ASSISTANT, "네", 100L), "role 차이")
        assertNotEquals(base, stableTranscriptId(History.Role.USER, "아니오", 100L), "content 차이")
        assertNotEquals(base, stableTranscriptId(History.Role.USER, "네", 101L), "ts 차이")
    }

    @Test
    fun transcriptRowsAreStableWhileLiveRowsAreUnique() {
        // A transcript-sourced row uses the deterministic id; two independently
        // rebuilt rows for the same message collide (good — reused). A fresh live
        // row (default UUID) never collides with them.
        val ts = 1_700_000_000_000L
        val reload1 = History(id = stableTranscriptId(History.Role.ASSISTANT, "답변", ts), role = History.Role.ASSISTANT, content = "답변", timestampMs = ts)
        val reload2 = History(id = stableTranscriptId(History.Role.ASSISTANT, "답변", ts), role = History.Role.ASSISTANT, content = "답변", timestampMs = ts)
        val live = History(role = History.Role.ASSISTANT, content = "답변", timestampMs = ts)
        assertEquals(reload1.id, reload2.id, "재로드 간 id 안정")
        assertNotEquals(reload1.id, live.id, "라이브 낙관 행은 별도 id")
    }
}
