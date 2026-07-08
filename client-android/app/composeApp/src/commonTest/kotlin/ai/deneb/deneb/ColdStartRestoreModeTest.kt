package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals

class ColdStartRestoreModeTest {
    @Test
    fun `client main opens home transcript`() {
        assertEquals(ColdStartRestoreMode.HOME, coldStartRestoreMode("client:main"))
    }

    @Test
    fun `explicit child conversation restores its transcript`() {
        assertEquals(ColdStartRestoreMode.TRANSCRIPT, coldStartRestoreMode("client:main:8f3a-uuid"))
    }

    @Test
    fun `legacy chat conversation still restores its transcript`() {
        assertEquals(ColdStartRestoreMode.TRANSCRIPT, coldStartRestoreMode("chat:legacy-uuid"))
    }

    @Test
    fun `opened machine session restores its transcript`() {
        assertEquals(ColdStartRestoreMode.TRANSCRIPT, coldStartRestoreMode("system:heartbeat"))
    }

    @Test
    fun `blank persisted session restores nothing`() {
        assertEquals(ColdStartRestoreMode.NONE, coldStartRestoreMode(""))
    }
}
