package ai.deneb.contacts

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class ContactsReaderTest {

    @Test
    fun desktopReaderReportsUnsupportedAndDeniedState() {
        val reader = ContactsReader()

        assertFalse(reader.isSupported())
        assertFalse(reader.hasAccess())
    }

    @Test
    fun desktopReaderReturnsEmptyStateWithoutContactAccess() = runTest {
        val reader = ContactsReader()

        val contacts = reader.readAll()

        assertEquals(emptyList(), contacts)
        assertFalse(reader.hasAccess())
    }
}
