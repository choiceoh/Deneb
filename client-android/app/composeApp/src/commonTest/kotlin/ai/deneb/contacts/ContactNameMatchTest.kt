package ai.deneb.contacts

import kotlin.test.Test
import kotlin.test.assertEquals

class ContactNameMatchTest {
    @Test
    fun normalizePersonNameStripsAffiliationAndTitles() {
        assertEquals("박한주", normalizePersonName("#박한주 부장(솔라테크)"))
        assertEquals("박한주", normalizePersonName("박한주 팀장"))
    }

    @Test
    fun normalizePersonNameKeepsShortDistinctNames() {
        assertEquals("이수", normalizePersonName("이수"))
        assertEquals("이수민", normalizePersonName("이수민"))
    }
}
