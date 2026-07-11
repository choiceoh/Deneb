package ai.deneb.email

import ai.deneb.data.EmailAccount
import ai.deneb.data.EmailMessage
import ai.deneb.data.EmailStore
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

interface EmailPollingClient {
    suspend fun connect()
    suspend fun login(username: String, password: String): Boolean
    suspend fun selectInbox(): Int
    suspend fun searchUnseen(): List<Long>
    suspend fun fetchHeaders(uids: List<Long>, accountId: String): List<EmailMessage>
    suspend fun logout()
}

private class ImapEmailPollingClient(host: String, port: Int) : EmailPollingClient {
    private val delegate = ImapClient(host, port)

    override suspend fun connect() = delegate.connect()
    override suspend fun login(username: String, password: String): Boolean = delegate.login(username, password)
    override suspend fun selectInbox(): Int = delegate.selectInbox()
    override suspend fun searchUnseen(): List<Long> = delegate.searchUnseen()
    override suspend fun fetchHeaders(uids: List<Long>, accountId: String): List<EmailMessage> = delegate.fetchHeaders(uids, accountId)
    override suspend fun logout() = delegate.logout()
}

@OptIn(ExperimentalTime::class)
class EmailPoller(
    private val emailStore: EmailStore,
    private val imapClientFactory: (host: String, port: Int) -> EmailPollingClient = ::ImapEmailPollingClient,
) {
    private val pollMutex = Mutex()

    suspend fun poll(account: EmailAccount) = pollMutex.withLock {
        val syncState = emailStore.getSyncState(account.id)
        val attemptAt = Clock.System.now().toEpochMilliseconds()
        try {
            val password = emailStore.getPassword(account.id)
            val imap = imapClientFactory(account.imapHost, account.imapPort)
            try {
                imap.connect()
                check(imap.login(account.username.ifEmpty { account.email }, password)) { "IMAP login rejected" }
                imap.selectInbox()
                // searchUnseen returns UIDs ascending per RFC 3501, so filtering alone
                // keeps oldest-first ordering — overflow past MAX_FETCH_PER_POLL is
                // picked up on the next poll.
                val unseenUids = imap.searchUnseen()
                // lastSeenUid = highest UID already delivered to the user (via heartbeat
                // or check_email). Anything ≤ that has been surfaced and shouldn't return
                // to pending. Also skip UIDs already queued so back-to-back polls without
                // a heartbeat in between don't duplicate.
                val pendingUidsForAccount = emailStore.getPending()
                    .asSequence()
                    .filter { it.accountId == account.id }
                    .map { it.uid }
                    .toSet()
                val newUids = unseenUids
                    .distinct()
                    .filter { it > syncState.lastSeenUid && it !in pendingUidsForAccount }
                    .take(MAX_FETCH_PER_POLL)

                val updated = syncState.copy(
                    lastSyncEpochMs = attemptAt,
                    lastAttemptEpochMs = attemptAt,
                    unreadCount = unseenUids.size,
                    lastError = null,
                )
                if (newUids.isNotEmpty()) {
                    emailStore.addPending(
                        imap.fetchHeaders(newUids, account.id)
                            .distinctBy { it.accountId to it.uid },
                    )
                }
                emailStore.updateSyncState(updated)
            } finally {
                imap.logout()
            }
        } catch (c: CancellationException) {
            throw c
        } catch (e: Exception) {
            emailStore.updateSyncState(
                syncState.copy(
                    lastAttemptEpochMs = attemptAt,
                    lastError = e.message ?: e::class.simpleName ?: "Poll failed",
                ),
            )
        }
    }

    companion object {
        const val MAX_FETCH_PER_POLL = 50
    }
}
