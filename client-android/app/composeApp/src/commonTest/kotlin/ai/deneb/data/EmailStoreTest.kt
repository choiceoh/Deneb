package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class EmailStoreTest {

    private data class Fixture(
        val settings: AppSettings,
        val store: EmailStore,
    )

    private fun fixture(): Fixture {
        val settings = AppSettings(MapSettings())
        return Fixture(settings, EmailStore(settings))
    }

    private fun account(id: String, email: String = "$id@example.com") = EmailAccount(
        id = id,
        email = email,
        displayName = id.uppercase(),
        imapHost = "imap.example.com",
        smtpHost = "smtp.example.com",
        username = id,
    )

    private fun message(accountId: String, uid: Long, subject: String = "$accountId-$uid") = EmailMessage(
        uid = uid,
        accountId = accountId,
        from = "sender@example.com",
        subject = subject,
        preview = "preview",
    )

    @Test
    fun missingAndMalformedAccountsReadAsEmpty() {
        val f = fixture()
        assertEquals(emptyList(), f.store.getAccounts())

        for (raw in listOf("broken", "{}", "[1,2]")) {
            f.settings.setEmailAccountsJson(raw)
            assertEquals(emptyList(), f.store.getAccounts(), raw)
        }
    }

    @Test
    fun addAccountPersistsInsertionOrderAndLookup() = runTest {
        val f = fixture()
        val personal = account("personal")
        val work = account("work")

        assertEquals(personal, f.store.addAccount(personal))
        f.store.addAccount(work)

        assertEquals(listOf(personal, work), f.store.getAccounts())
        assertEquals(work, f.store.getAccount("work"))
        assertNull(f.store.getAccount("missing"))
        assertTrue(f.settings.getEmailAccountsJson().startsWith("["))
    }

    @Test
    fun addingExistingIdReplacesItAndMovesItToNewestPosition() = runTest {
        val f = fixture()
        f.store.addAccount(account("a", "old@example.com"))
        f.store.addAccount(account("b"))

        val replacement = account("a", "new@example.com")
        f.store.addAccount(replacement)

        assertEquals(listOf("b", "a"), f.store.getAccounts().map { it.id })
        assertEquals(replacement, f.store.getAccount("a"))
        assertEquals(2, f.store.getAccounts().size)
    }

    @Test
    fun concurrentAddsDoNotLoseAccounts() = runTest {
        val f = fixture()

        coroutineScope {
            repeat(20) { index ->
                launch { f.store.addAccount(account("account-$index")) }
            }
        }

        assertEquals(20, f.store.getAccounts().size)
        assertEquals(
            (0 until 20).map { "account-$it" }.toSet(),
            f.store.getAccounts().map { it.id }.toSet(),
        )
    }

    @Test
    fun passwordsAreStoredSeparatelyPerAccount() = runTest {
        val f = fixture()

        f.store.setPassword("a", "secret-a")
        f.store.setPassword("b", "secret-b")

        assertEquals("secret-a", f.store.getPassword("a"))
        assertEquals("secret-b", f.store.getPassword("b"))
        assertEquals("", f.store.getPassword("missing"))
        assertFalse(f.settings.getEmailAccountsJson().contains("secret"))
    }

    @Test
    fun syncStateDefaultsToRequestedAccountAfterMissingOrMalformedData() {
        val f = fixture()

        assertEquals(EmailSyncState(accountId = "a"), f.store.getSyncState("a"))

        f.settings.setEmailSyncStateJson("a", "broken")
        assertEquals(EmailSyncState(accountId = "a"), f.store.getSyncState("a"))
    }

    @Test
    fun syncStateRoundTripsAllOperationalFields() = runTest {
        val f = fixture()
        val state = EmailSyncState(
            accountId = "a",
            lastSeenUid = 99,
            lastSyncEpochMs = 1_000,
            unreadCount = 7,
            lastAttemptEpochMs = 1_100,
            lastError = "timeout",
        )

        f.store.updateSyncState(state)

        assertEquals(state, f.store.getSyncState("a"))
        assertEquals(state, EmailStore(f.settings).getSyncState("a"))
    }

    @Test
    fun allSyncStatesFollowAccountOrderAndIgnoreOrphans() = runTest {
        val f = fixture()
        f.store.updateSyncState(EmailSyncState(accountId = "orphan", unreadCount = 100))
        f.store.addAccount(account("a"))
        f.store.addAccount(account("b"))
        f.store.updateSyncState(EmailSyncState(accountId = "b", unreadCount = 2))

        val states = f.store.getAllSyncStates()

        assertEquals(listOf("a", "b"), states.keys.toList())
        assertEquals(0, states.getValue("a").unreadCount)
        assertEquals(2, states.getValue("b").unreadCount)
        assertFalse("orphan" in states)
    }

    @Test
    fun removingAccountCleansCredentialsSyncAndItsPendingMessages() = runTest {
        val f = fixture()
        f.store.addAccount(account("a"))
        f.store.addAccount(account("b"))
        f.store.setPassword("a", "secret")
        f.store.updateSyncState(EmailSyncState(accountId = "a", unreadCount = 4))
        f.store.addPending(listOf(message("a", 1), message("b", 1), message("a", 2)))

        assertTrue(f.store.removeAccount("a"))

        assertEquals(listOf("b"), f.store.getAccounts().map { it.id })
        assertEquals("", f.store.getPassword("a"))
        assertEquals(EmailSyncState(accountId = "a"), f.store.getSyncState("a"))
        assertEquals(listOf(message("b", 1)), f.store.getPending())
    }

    @Test
    fun removingMissingAccountLeavesRelatedStorageUntouched() = runTest {
        val f = fixture()
        f.store.setPassword("missing", "keep")
        f.store.updateSyncState(EmailSyncState(accountId = "missing", unreadCount = 3))

        assertFalse(f.store.removeAccount("missing"))

        assertEquals("keep", f.store.getPassword("missing"))
        assertEquals(3, f.store.getSyncState("missing").unreadCount)
    }

    @Test
    fun pendingRemovalUsesAccountAndUidCompositeIdentity() = runTest {
        val f = fixture()
        val a1 = message("a", 1)
        val b1 = message("b", 1)
        val a2 = message("a", 2)
        f.store.addPending(listOf(a1, b1, a2))

        f.store.removePending(listOf(a1.copy(subject = "different payload")))

        assertEquals(listOf(b1, a2), f.store.getPending())
    }

    @Test
    fun pendingQueueKeepsOnlyNewestHundredMessages() = runTest {
        val f = fixture()
        val messages = (1L..105L).map { uid -> message("a", uid) }

        f.store.addPending(messages)

        assertEquals(100, f.store.getPending().size)
        assertEquals((6L..105L).toList(), f.store.getPending().map { it.uid })
    }

    @Test
    fun malformedPendingStorageIsHealedOnNextAdd() = runTest {
        val f = fixture()
        f.settings.setEmailPendingJson("broken")

        f.store.addPending(listOf(message("a", 1)))

        assertEquals(listOf(message("a", 1)), f.store.getPending())
        assertTrue(f.settings.getEmailPendingJson().startsWith("["))
    }
}
