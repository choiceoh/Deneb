package skytycoon.ui.desktop

import skytycoon.ui.SaveSlot
import java.io.File
import kotlin.test.AfterTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** 세이브 교체는 실패하더라도 직전 판이 남아 있어야 한다 — 유일한 복구본이기 때문이다. */
class FileSaveStoreTest {

    private val dir = File(System.getProperty("java.io.tmpdir"), "skytycoon-test-${System.nanoTime()}")
    private val store = FileSaveStore(dir)

    @AfterTest
    fun cleanup() {
        dir.deleteRecursively()
    }

    @Test
    fun `덮어쓰기에 성공하면 새 내용만 남고 임시 파일은 치운다`() {
        assertTrue(store.write(SaveSlot.MANUAL, "첫 판"))
        assertTrue(store.write(SaveSlot.MANUAL, "둘째 판"))

        assertEquals("둘째 판", store.read(SaveSlot.MANUAL))
        val leftovers = dir.listFiles()!!.filter { it.name.endsWith(".tmp") || it.name.endsWith(".bak") }
        assertTrue(leftovers.isEmpty(), "임시 파일이 남았다: ${leftovers.map { it.name }}")
    }

    @Test
    fun `교체가 실패하면 직전 세이브를 되돌려 놓는다`() {
        assertTrue(store.write(SaveSlot.MANUAL, "소중한 캠페인"))
        val target = File(dir, "manual.json")

        // 원본이 없는 tmp 로 교체를 시도하면 두 번의 rename 이 모두 실패한다 —
        // 기존 세이브를 지우고 시작했다면 이 시점에 판이 사라진다.
        val ghost = File(dir, "manual.json.tmp")
        assertFalse(ghost.exists())
        assertFailsWith<IllegalStateException> { store.replaceViaBackup(ghost, target) }

        assertEquals("소중한 캠페인", store.read(SaveSlot.MANUAL), "교체 실패 후 직전 세이브가 사라졌다")
        assertFalse(File(dir, "manual.json.bak").exists(), "백업 파일이 남았다")
    }

    @Test
    fun `교체 도중 죽어 백업만 남아도 읽어낸다`() {
        assertTrue(store.write(SaveSlot.MANUAL, "20년짜리 캠페인"))
        // 백업으로 옮긴 직후 프로세스가 죽은 상황 — 대상 파일이 없고 .bak 만 있다.
        val target = File(dir, "manual.json")
        assertTrue(target.renameTo(File(dir, "manual.json.bak")))
        assertFalse(target.exists())

        assertEquals("20년짜리 캠페인", store.read(SaveSlot.MANUAL), "백업만 남은 세이브를 못 찾았다")

        // 지운 슬롯이 백업 때문에 되살아나서는 안 된다.
        store.clear(SaveSlot.MANUAL)
        assertEquals(null, store.read(SaveSlot.MANUAL), "지운 슬롯이 백업에서 되살아났다")
    }

    @Test
    fun `슬롯끼리 서로를 덮어쓰지 않는다`() {
        store.write(SaveSlot.AUTO, "자동")
        store.write(SaveSlot.MANUAL, "수동")
        assertEquals("자동", store.read(SaveSlot.AUTO))
        assertEquals("수동", store.read(SaveSlot.MANUAL))

        store.clear(SaveSlot.MANUAL)
        assertEquals("자동", store.read(SaveSlot.AUTO))
        assertEquals(null, store.read(SaveSlot.MANUAL))
    }
}
