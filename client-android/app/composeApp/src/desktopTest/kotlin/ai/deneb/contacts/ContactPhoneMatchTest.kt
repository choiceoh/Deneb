package ai.deneb.contacts

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ContactPhoneMatchTest {

    @Test
    fun normalizeMapsInternationalAndNationalVariantsToSameKey() {
        assertEquals("01012345678", normalizePhoneForIdentityMatch("+82 10-1234-5678"))
        assertEquals("01012345678", normalizePhoneForIdentityMatch("010-1234-5678"))
    }

    @Test
    fun normalizeKeepsDistinctNumbersSeparate() {
        val mobile = normalizePhoneForIdentityMatch("010-1234-5678")
        val landline = normalizePhoneForIdentityMatch("02-1234-5678")
        assertEquals("01012345678", mobile)
        assertEquals("0212345678", landline)
        assertFalse(mobile == landline, "last-8 collision must not collapse different numbers")
    }

    @Test
    fun personalPhoneGateMatchesGatewayDedup() {
        assertTrue(isPersonalPhoneKey("01012345678"))
        assertFalse(isPersonalPhoneKey("12345"))
    }
}
