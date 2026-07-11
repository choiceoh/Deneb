package ai.deneb.email

import ai.deneb.data.AppSettings
import ai.deneb.data.EmailAccount
import ai.deneb.data.EmailMessage
import ai.deneb.data.EmailStore
import ai.deneb.data.EmailSyncState
import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.Clock

class EmailPollerStateMachineTest {

    private class FakeClient : EmailPollingClient {
        val calls = mutableListOf<String>()
        val loginArgs = mutableListOf<Pair<String, String>>()
        val fetchArgs = mutableListOf<Pair<List<Long>, String>>()
        var loginResult = true
        var inboxCount = 0
        var unseen = emptyList<Long>()
        var fetched = emptyList<EmailMessage>()
        var connectFailure: Throwable? = null
        var loginFailure: Throwable? = null
        var selectFailure: Throwable? = null
        var searchFailure: Throwable? = null
        var fetchFailure: Throwable? = null
        var logoutFailure: Throwable? = null
        var searchGate: CompletableDeferred<Unit>? = null
        var activeSearches = 0
        var maxActiveSearches = 0

        override suspend fun connect() {
            calls += "connect"
            connectFailure?.let { throw it }
        }

        override suspend fun login(username: String, password: String): Boolean {
            calls += "login"
            loginArgs += username to password
            loginFailure?.let { throw it }
            return loginResult
        }

        override suspend fun selectInbox(): Int {
            calls += "select"
            selectFailure?.let { throw it }
            return inboxCount
        }

        override suspend fun searchUnseen(): List<Long> {
            calls += "search"
            activeSearches++
            maxActiveSearches = maxOf(maxActiveSearches, activeSearches)
            try {
                searchGate?.await()
                searchFailure?.let { throw it }
                return unseen
            } finally {
                activeSearches--
            }
        }

        override suspend fun fetchHeaders(uids: List<Long>, accountId: String): List<EmailMessage> {
            calls += "fetch"
            fetchArgs += uids to accountId
            fetchFailure?.let { throw it }
            return fetched
        }

        override suspend fun logout() {
            calls += "logout"
            logoutFailure?.let { throw it }
        }
    }

    private data class Fixture(
        val settings: AppSettings,
        val store: EmailStore,
        val client: FakeClient,
        val factoryArgs: MutableList<Pair<String, Int>>,
        val poller: EmailPoller,
    )

    private fun fixture(client: FakeClient = FakeClient()): Fixture {
        val settings = AppSettings(MapSettings())
        val store = EmailStore(settings)
        val factoryArgs = mutableListOf<Pair<String, Int>>()
        val poller = EmailPoller(store) { host, port ->
            factoryArgs += host to port
            client
        }
        return Fixture(settings, store, client, factoryArgs, poller)
    }

    private fun account(
        id: String = "account-1",
        email: String = "user@example.com",
        username: String = "",
        host: String = "imap.example.com",
        port: Int = 993,
    ) = EmailAccount(
        id = id,
        email = email,
        imapHost = host,
        imapPort = port,
        smtpHost = "smtp.example.com",
        username = username,
    )

    private fun message(uid: Long, accountId: String = "account-1", read: Boolean = false) = EmailMessage(
        uid = uid,
        accountId = accountId,
        from = "sender-$uid@example.com",
        subject = "subject-$uid",
        isRead = read,
    )

    @Test
    fun factoryReceivesConfiguredHostAndPort() = runTest {
        val f = fixture()

        f.poller.poll(account(host = "mail.internal", port = 1143))

        assertEquals(listOf("mail.internal" to 1143), f.factoryArgs)
    }

    @Test
    fun emptyUsernameFallsBackToAccountEmail() = runTest {
        val f = fixture()
        f.store.setPassword("account-1", "secret")

        f.poller.poll(account(email = "fallback@example.com", username = ""))

        assertEquals(listOf("fallback@example.com" to "secret"), f.client.loginArgs)
    }

    @Test
    fun explicitUsernameWinsOverEmailAddress() = runTest {
        val f = fixture()
        f.store.setPassword("account-1", "secret")

        f.poller.poll(account(email = "mail@example.com", username = "login-name"))

        assertEquals("login-name" to "secret", f.client.loginArgs.single())
    }

    @Test
    fun successfulPollRunsProtocolInOrderAndAlwaysLogsOut() = runTest {
        val f = fixture()

        f.poller.poll(account())

        assertEquals(listOf("connect", "login", "select", "search", "logout"), f.client.calls)
    }

    @Test
    fun successfulEmptyPollUpdatesBothTimestampsAndClearsError() = runTest {
        val f = fixture()
        f.store.updateSyncState(EmailSyncState("account-1", lastSeenUid = 7, lastError = "old error"))
        val before = Clock.System.now().toEpochMilliseconds()

        f.poller.poll(account())
        val after = Clock.System.now().toEpochMilliseconds()
        val state = f.store.getSyncState("account-1")

        assertTrue(state.lastSyncEpochMs in before..after)
        assertEquals(state.lastSyncEpochMs, state.lastAttemptEpochMs)
        assertNull(state.lastError)
        assertEquals(0, state.unreadCount)
        assertEquals(7L, state.lastSeenUid)
    }

    @Test
    fun unreadCountReflectsWholeServerUnseenSetNotOnlyNewRows() = runTest {
        val f = fixture()
        f.client.unseen = listOf(1, 2, 3, 4)
        f.store.updateSyncState(EmailSyncState("account-1", lastSeenUid = 3))
        f.client.fetched = listOf(message(4))

        f.poller.poll(account())

        assertEquals(4, f.store.getSyncState("account-1").unreadCount)
        assertEquals(listOf(4L), f.client.fetchArgs.single().first)
    }

    @Test
    fun uidsAtOrBelowLastSeenAreNotFetched() = runTest {
        val f = fixture()
        f.client.unseen = listOf(1, 2, 3, 4, 5)
        f.store.updateSyncState(EmailSyncState("account-1", lastSeenUid = 3))
        f.client.fetched = listOf(message(4), message(5))

        f.poller.poll(account())

        assertEquals(listOf(4L, 5L), f.client.fetchArgs.single().first)
    }

    @Test
    fun alreadyPendingUidsForSameAccountAreNotFetchedAgain() = runTest {
        val f = fixture()
        f.client.unseen = listOf(10, 11, 12)
        f.store.addPending(listOf(message(11)))
        f.client.fetched = listOf(message(10), message(12))

        f.poller.poll(account())

        assertEquals(listOf(10L, 12L), f.client.fetchArgs.single().first)
    }

    @Test
    fun pendingUidFromAnotherAccountDoesNotSuppressFetch() = runTest {
        val f = fixture()
        f.client.unseen = listOf(10)
        f.store.addPending(listOf(message(10, accountId = "other")))
        f.client.fetched = listOf(message(10))

        f.poller.poll(account())

        assertEquals(listOf(10L), f.client.fetchArgs.single().first)
        assertEquals(setOf("other", "account-1"), f.store.getPending().map { it.accountId }.toSet())
    }

    @Test
    fun duplicateServerUidsAreCollapsedBeforeFetch() = runTest {
        val f = fixture()
        f.client.unseen = listOf(7, 7, 8, 7, 8)
        f.client.fetched = listOf(message(7), message(8))

        f.poller.poll(account())

        assertEquals(listOf(7L, 8L), f.client.fetchArgs.single().first)
        assertEquals(5, f.store.getSyncState("account-1").unreadCount)
    }

    @Test
    fun fetchIsCappedAtFiftyUidsInServerOrder() = runTest {
        val f = fixture()
        f.client.unseen = (1L..80L).toList()
        f.client.fetched = (1L..50L).map(::message)

        f.poller.poll(account())

        assertEquals((1L..50L).toList(), f.client.fetchArgs.single().first)
        assertEquals(50, f.store.getPending().size)
    }

    @Test
    fun noEligibleUidSkipsFetchHeadersEntirely() = runTest {
        val f = fixture()
        f.client.unseen = listOf(1, 2)
        f.store.updateSyncState(EmailSyncState("account-1", lastSeenUid = 2))

        f.poller.poll(account())

        assertTrue(f.client.fetchArgs.isEmpty())
        assertFalse("fetch" in f.client.calls)
    }

    @Test
    fun fetchedHeadersArePersistedInReturnedOrder() = runTest {
        val f = fixture()
        f.client.unseen = listOf(3, 4)
        f.client.fetched = listOf(message(4), message(3))

        f.poller.poll(account())

        assertEquals(listOf(4L, 3L), f.store.getPending().map { it.uid })
    }

    @Test
    fun duplicateFetchedHeadersAreDeduplicatedByStoreIdentity() = runTest {
        val f = fixture()
        f.client.unseen = listOf(3)
        f.client.fetched = listOf(message(3), message(3))

        f.poller.poll(account())

        assertEquals(listOf(3L), f.store.getPending().map { it.uid })
    }

    @Test
    fun rejectedLoginStopsBeforeMailboxSelectionAndRecordsFailure() = runTest {
        val f = fixture()
        f.client.loginResult = false

        f.poller.poll(account())

        assertEquals(listOf("connect", "login", "logout"), f.client.calls)
        assertEquals("IMAP login rejected", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun factoryFailureRecordsErrorWithoutLogout() = runTest {
        val settings = AppSettings(MapSettings())
        val store = EmailStore(settings)
        val poller = EmailPoller(store) { _, _ -> error("factory failed") }

        poller.poll(account())

        assertEquals("factory failed", store.getSyncState("account-1").lastError)
    }

    @Test
    fun connectFailureStillAttemptsBestEffortLogout() = runTest {
        val f = fixture()
        f.client.connectFailure = IllegalStateException("connect failed")

        f.poller.poll(account())

        assertEquals(listOf("connect", "logout"), f.client.calls)
        assertEquals("connect failed", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun selectFailureRecordsErrorAndLogsOut() = runTest {
        val f = fixture()
        f.client.selectFailure = IllegalArgumentException("select failed")

        f.poller.poll(account())

        assertEquals(listOf("connect", "login", "select", "logout"), f.client.calls)
        assertEquals("select failed", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun searchFailurePreservesPreviousSyncProgress() = runTest {
        val f = fixture()
        val previous = EmailSyncState("account-1", lastSeenUid = 44, lastSyncEpochMs = 55, unreadCount = 6)
        f.store.updateSyncState(previous)
        f.client.searchFailure = IllegalStateException("search failed")

        f.poller.poll(account())
        val state = f.store.getSyncState("account-1")

        assertEquals(44L, state.lastSeenUid)
        assertEquals(55L, state.lastSyncEpochMs)
        assertEquals(6, state.unreadCount)
        assertEquals("search failed", state.lastError)
        assertTrue(state.lastAttemptEpochMs > 0)
    }

    @Test
    fun fetchFailureDoesNotLeavePartiallyAddedPendingRows() = runTest {
        val f = fixture()
        f.client.unseen = listOf(1)
        f.client.fetchFailure = IllegalStateException("fetch failed")

        f.poller.poll(account())

        assertEquals(emptyList(), f.store.getPending())
        assertEquals("fetch failed", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun logoutFailureConvertsOtherwiseSuccessfulAttemptToFailure() = runTest {
        val f = fixture()
        f.client.logoutFailure = IllegalStateException("logout failed")

        f.poller.poll(account())

        assertEquals("logout failed", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun exceptionWithoutMessageFallsBackToClassName() = runTest {
        val f = fixture()
        f.client.searchFailure = Exception()

        f.poller.poll(account())

        assertEquals("Exception", f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun cancellationDuringConnectPropagatesAndStillLogsOut() = runTest {
        val f = fixture()
        f.client.connectFailure = CancellationException("cancel connect")

        val failure = assertFailsWith<CancellationException> { f.poller.poll(account()) }

        assertEquals("cancel connect", failure.message)
        assertEquals(listOf("connect", "logout"), f.client.calls)
        assertEquals(EmailSyncState("account-1"), f.store.getSyncState("account-1"))
    }

    @Test
    fun cancellationDuringSearchPropagatesWithoutWritingErrorState() = runTest {
        val f = fixture()
        val previous = EmailSyncState("account-1", lastSeenUid = 9, lastSyncEpochMs = 8)
        f.store.updateSyncState(previous)
        f.client.searchFailure = CancellationException("cancel search")

        assertFailsWith<CancellationException> { f.poller.poll(account()) }

        assertEquals(previous, f.store.getSyncState("account-1"))
        assertEquals("logout", f.client.calls.last())
    }

    @Test
    fun cancellationDuringFetchPropagatesWithoutPersistingFetchedRows() = runTest {
        val f = fixture()
        f.client.unseen = listOf(1)
        f.client.fetchFailure = CancellationException("cancel fetch")

        assertFailsWith<CancellationException> { f.poller.poll(account()) }

        assertEquals(emptyList(), f.store.getPending())
        assertNull(f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun concurrentPollsOnSamePollerNeverOverlapProtocolSearches() = runTest {
        val f = fixture()
        f.client.searchGate = CompletableDeferred()

        val first = async { f.poller.poll(account()) }
        val second = async { f.poller.poll(account()) }
        testScheduler.runCurrent()

        assertEquals(1, f.client.activeSearches)
        assertEquals(1, f.factoryArgs.size)

        f.client.searchGate?.complete(Unit)
        first.await()
        second.await()
        assertEquals(1, f.client.maxActiveSearches)
        assertEquals(2, f.factoryArgs.size)
    }

    @Test
    fun cancellingQueuedSecondPollDoesNotCancelActiveFirstPoll() = runTest {
        val f = fixture()
        f.client.searchGate = CompletableDeferred()
        val first = async { f.poller.poll(account()) }
        val second = async { f.poller.poll(account()) }
        testScheduler.runCurrent()

        second.cancel()
        f.client.searchGate?.complete(Unit)
        first.await()

        assertTrue(second.isCancelled)
        assertEquals(1, f.factoryArgs.size)
        assertNull(f.store.getSyncState("account-1").lastError)
    }

    @Test
    fun negativeAndPreviouslySeenUidsAreFilteredAtZeroCursor() = runTest {
        val f = fixture()
        f.client.unseen = listOf(Long.MIN_VALUE, -1, 0, 1)
        f.client.fetched = listOf(message(1))

        f.poller.poll(account())

        assertEquals(listOf(1L), f.client.fetchArgs.single().first)
    }

    @Test
    fun maximumUidCanBeFetchedWithoutOverflow() = runTest {
        val f = fixture()
        f.client.unseen = listOf(Long.MAX_VALUE)
        f.client.fetched = listOf(message(Long.MAX_VALUE))

        f.poller.poll(account())

        assertEquals(listOf(Long.MAX_VALUE), f.store.getPending().map { it.uid })
    }
}
