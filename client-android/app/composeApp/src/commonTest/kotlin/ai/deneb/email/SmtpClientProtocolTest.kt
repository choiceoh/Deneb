package ai.deneb.email

import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SmtpClientProtocolTest {

    private class FakeConnection(
        responses: List<String>,
    ) : EmailConnection {
        val responses = ArrayDeque(responses)
        val writes = mutableListOf<String>()
        val tlsHosts = mutableListOf<String>()
        var closeCalls = 0
        var failClose = false

        override suspend fun readLine(): String = responses.removeFirstOrNull()
            ?: error("no scripted SMTP response")

        override suspend fun writeLine(line: String) {
            writes += line
        }

        override suspend fun upgradeToTls(host: String) {
            tlsHosts += host
        }

        override suspend fun close() {
            closeCalls++
            if (failClose) error("close failed")
        }
    }

    private data class Fixture(
        val client: SmtpClient,
        val connection: FakeConnection,
        val factoryCalls: MutableList<Triple<String, Int, Boolean>>,
    )

    private fun fixture(
        responses: List<String>,
        host: String = "smtp.example",
        port: Int = 587,
        startTls: Boolean = true,
    ): Fixture {
        val connection = FakeConnection(responses)
        val calls = mutableListOf<Triple<String, Int, Boolean>>()
        val client = SmtpClient(host, port, startTls) { requestedHost, requestedPort, tls ->
            calls += Triple(requestedHost, requestedPort, tls)
            connection
        }
        return Fixture(client, connection, calls)
    }

    @Test
    fun protocolMethodsRejectUseBeforeConnect() = runTest {
        val f = fixture(emptyList())

        assertFailsWith<IllegalStateException> { f.client.ehlo() }
        assertFailsWith<IllegalStateException> { f.client.authenticate("u", "p") }
        assertFailsWith<IllegalStateException> { f.client.sendReply("a", "b", "s", "body") }
    }

    @Test
    fun connectRequestsPlainSocketWhenStartTlsWillUpgradeLater() = runTest {
        val f = fixture(listOf("220 smtp ready"), port = 2525, startTls = true)

        f.client.connect()

        assertEquals(listOf(Triple("smtp.example", 2525, false)), f.factoryCalls)
    }

    @Test
    fun connectRequestsImplicitTlsWhenStartTlsIsDisabled() = runTest {
        val f = fixture(listOf("220 smtp ready"), port = 465, startTls = false)

        f.client.connect()

        assertEquals(listOf(Triple("smtp.example", 465, true)), f.factoryCalls)
    }

    @Test
    fun connectConsumesGreetingWithoutWritingACommand() = runTest {
        val f = fixture(listOf("220 smtp ready"))

        f.client.connect()

        assertEquals(emptyList(), f.connection.writes)
        assertEquals(0, f.connection.responses.size)
    }

    @Test
    fun factoryFailurePropagatesAndLeavesClientDisconnected() = runTest {
        val client = SmtpClient("smtp.example", connectionFactory = { _, _, _ -> error("dial failed") })

        assertFailsWith<IllegalStateException> { client.connect() }
        assertFailsWith<IllegalStateException> { client.ehlo() }
    }

    @Test
    fun ehloUsesDefaultLocalhostDomain() = runTest {
        val f = fixture(listOf("220 ready", "250 hello"))
        f.client.connect()

        f.client.ehlo()

        assertEquals(listOf("EHLO localhost"), f.connection.writes)
    }

    @Test
    fun ehloUsesCallerProvidedDomain() = runTest {
        val f = fixture(listOf("220 ready", "250 hello"))
        f.client.connect()

        f.client.ehlo("client.example")

        assertEquals(listOf("EHLO client.example"), f.connection.writes)
    }

    @Test
    fun ehloConsumesMultilineResponseThroughFinalSpaceLine() = runTest {
        val f = fixture(listOf("220 ready", "250-PIPELINING", "250-SIZE 1000", "250 AUTH LOGIN"))
        f.client.connect()

        f.client.ehlo()

        assertEquals(0, f.connection.responses.size)
    }

    @Test
    fun disabledStartTlsIsANoOp() = runTest {
        val f = fixture(listOf("220 ready"), startTls = false)
        f.client.connect()

        f.client.startTls()

        assertEquals(emptyList(), f.connection.writes)
        assertEquals(emptyList(), f.connection.tlsHosts)
    }

    @Test
    fun successfulStartTlsUpgradesAndReissuesEhlo() = runTest {
        val f = fixture(listOf("220 greeting", "220 Ready to start TLS", "250 secure hello"))
        f.client.connect()

        f.client.startTls()

        assertEquals(listOf("STARTTLS", "EHLO localhost"), f.connection.writes)
        assertEquals(listOf("smtp.example"), f.connection.tlsHosts)
    }

    @Test
    fun rejectedStartTlsDoesNotUpgradeOrEhlo() = runTest {
        val f = fixture(listOf("220 greeting", "454 TLS unavailable"))
        f.client.connect()

        val failure = assertFailsWith<Exception> { f.client.startTls() }

        assertTrue("454 TLS unavailable" in failure.message.orEmpty())
        assertEquals(listOf("STARTTLS"), f.connection.writes)
        assertEquals(emptyList(), f.connection.tlsHosts)
    }

    @Test
    fun authenticationWritesLoginAndBase64Credentials() = runTest {
        val f = fixture(listOf("220 ready", "334 username", "334 password", "235 authenticated"))
        f.client.connect()

        f.client.authenticate("user", "secret")

        assertEquals(listOf("AUTH LOGIN", "dXNlcg==", "c2VjcmV0"), f.connection.writes)
    }

    @Test
    fun authenticationBase64EncodesUtf8Credentials() = runTest {
        val f = fixture(listOf("220 ready", "334 username", "334 password", "235 authenticated"))
        f.client.connect()

        f.client.authenticate("사용자", "암호")

        assertEquals("7IKs7Jqp7J6Q", f.connection.writes[1])
        assertEquals("7JWU7Zi4", f.connection.writes[2])
    }

    @Test
    fun unsupportedAuthLoginStopsBeforeCredentials() = runTest {
        val f = fixture(listOf("220 ready", "504 auth unsupported"))
        f.client.connect()

        assertFailsWith<Exception> { f.client.authenticate("user", "secret") }

        assertEquals(listOf("AUTH LOGIN"), f.connection.writes)
    }

    @Test
    fun rejectedUsernameStopsBeforePassword() = runTest {
        val f = fixture(listOf("220 ready", "334 username", "535 bad user"))
        f.client.connect()

        assertFailsWith<Exception> { f.client.authenticate("user", "secret") }

        assertEquals(listOf("AUTH LOGIN", "dXNlcg=="), f.connection.writes)
    }

    @Test
    fun rejectedPasswordReportsAuthenticationFailure() = runTest {
        val f = fixture(listOf("220 ready", "334 username", "334 password", "535 bad password"))
        f.client.connect()

        val failure = assertFailsWith<Exception> { f.client.authenticate("user", "secret") }

        assertTrue("Authentication failed" in failure.message.orEmpty())
        assertEquals(3, f.connection.writes.size)
    }

    @Test
    fun sendReplyRunsEnvelopeDataAndTerminatorInOrder() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 send data", "250 queued"))
        f.client.connect()

        assertTrue(f.client.sendReply("from@example.com", "to@example.com", "Subject", "Body"))

        assertEquals("MAIL FROM:<from@example.com>", f.connection.writes[0])
        assertEquals("RCPT TO:<to@example.com>", f.connection.writes[1])
        assertEquals("DATA", f.connection.writes[2])
        assertEquals(".", f.connection.writes.last())
    }

    @Test
    fun sendReplyWritesRequiredHeadersAndBlankSeparator() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "250 queued"))
        f.client.connect()

        f.client.sendReply("from@example.com", "to@example.com", "Status", "Body")

        val data = f.connection.writes.drop(3).dropLast(1)
        assertTrue("From: from@example.com" in data)
        assertTrue("To: to@example.com" in data)
        assertTrue("Subject: Status" in data)
        assertTrue("MIME-Version: 1.0" in data)
        assertTrue("Content-Type: text/plain; charset=UTF-8" in data)
        assertTrue("" in data)
        assertTrue("Body" in data)
    }

    @Test
    fun replyReferenceAddsBothThreadingHeaders() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "250 queued"))
        f.client.connect()

        f.client.sendReply("from", "to", "subject", "body", inReplyTo = "<message@example.com>")

        assertTrue("In-Reply-To: <message@example.com>" in f.connection.writes)
        assertTrue("References: <message@example.com>" in f.connection.writes)
    }

    @Test
    fun absentReplyReferenceOmitsThreadingHeaders() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "250 queued"))
        f.client.connect()

        f.client.sendReply("from", "to", "subject", "body")

        assertFalse(f.connection.writes.any { it.startsWith("In-Reply-To:") })
        assertFalse(f.connection.writes.any { it.startsWith("References:") })
    }

    @Test
    fun bodyLinesBeginningWithDotsAreDotStuffed() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "250 queued"))
        f.client.connect()

        f.client.sendReply("from", "to", "subject", ".first\n..second\nordinary")

        assertTrue("..first" in f.connection.writes)
        assertTrue("...second" in f.connection.writes)
        assertTrue("ordinary" in f.connection.writes)
    }

    @Test
    fun nonSuccessQueueResponseReturnsFalse() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "554 rejected"))
        f.client.connect()

        assertFalse(f.client.sendReply("from", "to", "subject", "body"))
    }

    @Test
    fun senderRejectionStopsBeforeRecipientCommand() = runTest {
        val f = fixture(listOf("220 ready", "550 sender rejected"))
        f.client.connect()

        assertFailsWith<Exception> { f.client.sendReply("from", "to", "subject", "body") }

        assertEquals(listOf("MAIL FROM:<from>"), f.connection.writes)
    }

    @Test
    fun recipientRejectionStopsBeforeDataCommand() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "550 recipient rejected"))
        f.client.connect()

        assertFailsWith<Exception> { f.client.sendReply("from", "to", "subject", "body") }

        assertEquals(listOf("MAIL FROM:<from>", "RCPT TO:<to>"), f.connection.writes)
    }

    @Test
    fun dataRejectionStopsBeforeMessageHeaders() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "554 no data"))
        f.client.connect()

        assertFailsWith<Exception> { f.client.sendReply("from", "to", "subject", "body") }

        assertEquals(listOf("MAIL FROM:<from>", "RCPT TO:<to>", "DATA"), f.connection.writes)
    }

    @Test
    fun headerNewlinesAreCollapsedToPreventInjectedSmtpFields() = runTest {
        val f = fixture(listOf("220 ready", "250 sender", "250 recipient", "354 data", "250 queued"))
        f.client.connect()

        f.client.sendReply(
            from = "from@example.com\r\nBcc: attacker@example.com",
            to = "to@example.com\nCc: attacker@example.com",
            subject = "Status\r\nX-Injected: yes",
            body = "safe body",
            inReplyTo = "<id>\r\nX-Thread: bad",
        )

        assertFalse(f.connection.writes.any { it == "Bcc: attacker@example.com" })
        assertFalse(f.connection.writes.any { it == "Cc: attacker@example.com" })
        assertFalse(f.connection.writes.any { it == "X-Injected: yes" })
        assertFalse(f.connection.writes.any { it == "X-Thread: bad" })
        assertTrue("Subject: Status X-Injected: yes" in f.connection.writes)
    }

    @Test
    fun quitWritesCommandReadsReplyAndClosesConnection() = runTest {
        val f = fixture(listOf("220 ready", "221 bye"))
        f.client.connect()

        f.client.quit()

        assertEquals(listOf("QUIT"), f.connection.writes)
        assertEquals(1, f.connection.closeCalls)
    }

    @Test
    fun quitBeforeConnectIsHarmless() = runTest {
        val f = fixture(emptyList())

        f.client.quit()

        assertEquals(emptyList(), f.connection.writes)
        assertEquals(0, f.connection.closeCalls)
    }

    @Test
    fun quitFailureStillClearsConnectionState() = runTest {
        val f = fixture(listOf("220 ready", "221 bye"))
        f.client.connect()
        f.connection.failClose = true

        f.client.quit()
        f.client.quit()

        assertEquals(listOf("QUIT"), f.connection.writes)
        assertEquals(1, f.connection.closeCalls)
    }
}
