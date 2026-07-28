package ai.deneb.contacts

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The name gate decides who gets merged into whom on a real phone, so these pin the
 * behaviour case by case rather than in one forEach — a table that stops at the
 * first failure hides the rest, and every row here is a person.
 */
class PersonNameKeyTest {
    @Test
    fun stripsAffiliationAndTitleAndSortPrefix() {
        assertEquals("박한주", personMatchKey("#박한주 부장(솔라테크)"))
        assertEquals("박한주", personMatchKey("박한주"))
        assertEquals("오선택", personMatchKey("오선택 전무 (기획조정실장)"))
        assertEquals("김익환", personMatchKey("김익환 부장 (GS풍력발전)"))
    }

    @Test
    fun keepsShortNamesDistinct() {
        // Prefix bridging would fuse these two; exact-key matching must not.
        assertFalse(nameBelongsToGroup("이수민", setOf(personMatchKey("이수"))))
        assertFalse(nameBelongsToGroup("이수", setOf(personMatchKey("이수민"))))
    }

    @Test
    fun neverPeelsBelowTwoCharacters() {
        // Both are titles, but peeling leaves under two characters — keep as-is.
        // (Verified against the gateway: NormalizePersonName("부장") == "부장".)
        assertEquals("대표", personMatchKey("대표"))
        assertEquals("부장", personMatchKey("부장"))
    }

    @Test
    fun affiliationOnlyCardMatchesNothing() {
        // The empty-key case is a card that is ALL affiliation — everything before
        // the paren is blank. The gateway refuses to bridge on it because it would
        // transitively fuse two strangers who each merely share its number.
        assertEquals("", personMatchKey("(솔라테크)"))
        assertEquals("", personMatchKey("   "))
        assertTrue(groupMatchKeys(listOf("(솔라테크)", "")).isEmpty())
        assertFalse(nameBelongsToGroup("(솔라테크)", groupMatchKeys(listOf("박한주"))))
        // A title-only card has a non-empty key, so it simply fails to match a
        // different person's group — same outcome, different reason.
        assertFalse(nameBelongsToGroup("부장", groupMatchKeys(listOf("박한주"))))
    }

    @Test
    fun matchesGatewayNormalizePersonName() {
        // Pinned against the gateway's own output (go test, 2026-07-28). These two
        // implementations decide the same question on either side of the wire; if
        // this drifts, the device links the wrong people.
        assertEquals("박한주", personMatchKey("#박한주 부장(솔라테크)"))
        assertEquals("오선택", personMatchKey("오선택 전무 (기획조정실장)"))
        assertEquals("김익환", personMatchKey("김익환 부장 (GS풍력발전)"))
        assertEquals("선향란", personMatchKey("선향란 과장"))
        assertEquals("최종원", personMatchKey("최종원 차장"))
        assertEquals("홍홍정우", personMatchKey("홍홍정우"))
        assertEquals("이수", personMatchKey("이수"))
        assertEquals("이수민", personMatchKey("이수민"))
    }

    @Test
    fun bridgesFirstSyllableDoubling() {
        assertTrue(nameBelongsToGroup("홍홍정우", groupMatchKeys(listOf("홍정우"))))
        assertTrue(nameBelongsToGroup("홍정우", groupMatchKeys(listOf("홍홍정우"))))
    }

    @Test
    fun colleagueOnTheSameCompanyLineIsNotInTheGroup() {
        // The 2026-07-28 collapse in one assertion: a shared line matched everyone,
        // and without this gate every one of them was linked into one contact.
        val group = groupMatchKeys(listOf("박한주", "#박한주 부장(솔라테크)"))
        assertTrue(nameBelongsToGroup("박한주 부장", group))
        assertFalse(nameBelongsToGroup("김성훈", group))
        assertFalse(nameBelongsToGroup("최종원 차장", group))
    }
}
