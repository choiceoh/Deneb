package ai.deneb.email

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ServerAutoDetectTest {

    private data class Expected(
        val domain: String,
        val imap: String,
        val smtp: String,
        val smtpPort: Int = 587,
    )

    @Test
    fun detectsEveryHostedProviderEndpoint() {
        val providers = listOf(
            Expected("gmail.com", "imap.gmail.com", "smtp.gmail.com"),
            Expected("googlemail.com", "imap.gmail.com", "smtp.gmail.com"),
            Expected("outlook.com", "outlook.office365.com", "smtp.office365.com"),
            Expected("hotmail.com", "outlook.office365.com", "smtp.office365.com"),
            Expected("live.com", "outlook.office365.com", "smtp.office365.com"),
            Expected("yahoo.com", "imap.mail.yahoo.com", "smtp.mail.yahoo.com"),
            Expected("icloud.com", "imap.mail.me.com", "smtp.mail.me.com"),
            Expected("me.com", "imap.mail.me.com", "smtp.mail.me.com"),
            Expected("mac.com", "imap.mail.me.com", "smtp.mail.me.com"),
            Expected("aol.com", "imap.aol.com", "smtp.aol.com"),
            Expected("zoho.com", "imap.zoho.com", "smtp.zoho.com", smtpPort = 465),
            Expected("fastmail.com", "imap.fastmail.com", "smtp.fastmail.com"),
        )

        providers.forEach { expected ->
            val config = ServerAutoDetect.detect("person@${expected.domain}")
            requireNotNull(config)
            assertEquals(expected.imap, config.imapHost, expected.domain)
            assertEquals(993, config.imapPort, expected.domain)
            assertEquals(expected.smtp, config.smtpHost, expected.domain)
            assertEquals(expected.smtpPort, config.smtpPort, expected.domain)
            assertTrue(config.useStartTls, expected.domain)
        }
    }

    @Test
    fun providerAliasesResolveToIdenticalConfigurations() {
        assertEquals(ServerAutoDetect.detect("a@gmail.com"), ServerAutoDetect.detect("a@googlemail.com"))
        assertEquals(ServerAutoDetect.detect("a@outlook.com"), ServerAutoDetect.detect("a@hotmail.com"))
        assertEquals(ServerAutoDetect.detect("a@outlook.com"), ServerAutoDetect.detect("a@live.com"))
        assertEquals(ServerAutoDetect.detect("a@icloud.com"), ServerAutoDetect.detect("a@me.com"))
        assertEquals(ServerAutoDetect.detect("a@icloud.com"), ServerAutoDetect.detect("a@mac.com"))
        assertEquals(ServerAutoDetect.detect("a@protonmail.com"), ServerAutoDetect.detect("a@proton.me"))
    }

    @Test
    fun matchingIsCaseInsensitiveAndWhitespaceTolerant() {
        assertEquals(
            ServerAutoDetect.detect("person@gmail.com"),
            ServerAutoDetect.detect("  PERSON@GMAIL.COM  "),
        )
    }

    @Test
    fun protonUsesTheLocalBridgeWithoutStartTls() {
        val config = requireNotNull(ServerAutoDetect.detect("person@proton.me"))

        assertEquals("127.0.0.1", config.imapHost)
        assertEquals(1143, config.imapPort)
        assertEquals("127.0.0.1", config.smtpHost)
        assertEquals(1025, config.smtpPort)
        assertFalse(config.useStartTls)
        assertTrue(config.note.contains("Bridge"))
    }

    @Test
    fun appPasswordProvidersCarryOperatorGuidance() {
        for (domain in listOf("gmail.com", "yahoo.com", "icloud.com")) {
            val note = requireNotNull(ServerAutoDetect.detect("person@$domain")).note
            assertTrue(note.contains("Password"), domain)
        }
        assertEquals("", requireNotNull(ServerAutoDetect.detect("person@outlook.com")).note)
    }

    @Test
    fun unknownAndMalformedAddressesDoNotGuessAProvider() {
        for (email in listOf("", "gmail.com", "@gmail.com", "a@", "a@@gmail.com", "a@example.test")) {
            assertNull(ServerAutoDetect.detect(email), email)
        }
    }

    @Test
    fun subdomainsDoNotAccidentallyMatchParentProviders() {
        assertNull(ServerAutoDetect.detect("person@mail.gmail.com"))
        assertNull(ServerAutoDetect.detect("person@company.outlook.com"))
    }
}
