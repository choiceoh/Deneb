package ai.deneb.email

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ImapMessageParserTest {

    private val parser = ImapClient("mail.example")

    private fun imapParserCases(): List<() -> Unit> = listOf(
        {
            assertEquals("user@example.com", parser.escapeQuoted("user@example.com"))

        },
        {
            assertEquals("user\\\"name", parser.escapeQuoted("user\"name"))

        },
        {
            assertEquals("domain\\\\user", parser.escapeQuoted("domain\\user"))

        },
        {
            assertEquals("a\\\\\\\"b", parser.escapeQuoted("a\\\"b"))

        },
        {
            assertEquals("plain text", parser.decodeQuotedPrintable("plain text"))

        },
        {
            assertEquals("ABC!", parser.decodeQuotedPrintable("=41=42=43=21"))
            assertEquals("z~", parser.decodeQuotedPrintable("=7a=7E"))

        },
        {
            assertEquals("한글", parser.decodeQuotedPrintable("=ED=95=9C=EA=B8=80"))

        },
        {
            assertEquals("longline", parser.decodeQuotedPrintable("long=\nline"))

        },
        {
            assertEquals("longline", parser.decodeQuotedPrintable("long=\r\nline"))

        },
        {
            assertEquals("bad=GGvalue", parser.decodeQuotedPrintable("bad=GGvalue"))

        },
        {
            assertEquals("tail=", parser.decodeQuotedPrintable("tail="))
            assertEquals("tail=A", parser.decodeQuotedPrintable("tail=A"))

        },
        {
            assertEquals("한글!", parser.decodeQuotedPrintable("한글=21"))

        },
        {
            assertEquals("Hello", parser.decodeBase64OrOriginal("SGVsbG8="))

        },
        {
            assertEquals("Hello world", parser.decodeBase64OrOriginal("SGVs\n bG8gd29ybGQ="))

        },
        {
            assertEquals("한글", parser.decodeBase64OrOriginal("7ZWc6riA"))

        },
        {
            val original = "%%% not base64 %%%"

            assertEquals(original, parser.decodeBase64OrOriginal(original))

        },
        {
            assertEquals("", parser.decodeBase64OrOriginal(""))

        },
        {
            assertEquals("boundary_123", parser.detectMimeBoundary("preamble\n--boundary_123\npart"))

        },
        {
            assertEquals("mix+ed / value=?", parser.detectMimeBoundary("--mix+ed / value=?  \n"))

        },
        {
            assertNull(parser.detectMimeBoundary("prefix --boundary suffix"))
            assertNull(parser.detectMimeBoundary("ordinary body"))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: 8bit\n\nbody\nline two"

            assertEquals("body\nline two", parser.extractPartBody(part))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: quoted-printable\n\nhello=20world=21"

            assertEquals("hello world!", parser.extractPartBody(part))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: base64\n\n7ZWc6riA"

            assertEquals("한글", parser.extractPartBody(part))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: 8bit\nbody starts immediately"

            assertEquals("body starts immediately", parser.extractPartBody(part))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: x-custom\n\nraw=20body"

            assertEquals("raw=20body", parser.extractPartBody(part))

        },
        {
            val part = "Content-Type: text/plain\nContent-Transfer-Encoding: base64\n\n%%% broken %%%"

            assertEquals("%%% broken %%%", parser.extractPartBody(part))

        },
        {
            val content = """
                --b
                Content-Type: text/plain

                plain body
                --b
                Content-Type: text/html

                <p>html body</p>
                --b--
            """.trimIndent()

            assertEquals("plain body" to "<p>html body</p>", parser.extractPartsFromMultipart(content, "b"))

        },
        {
            val content = """
                --b
                Content-Type: text/html

                <strong>only html</strong>
                --b--
            """.trimIndent()

            assertEquals("" to "<strong>only html</strong>", parser.extractPartsFromMultipart(content, "b"))

        },
        {
            val content = """
                --b
                Content-Type: text/plain

                first plain
                --b
                Content-Type: text/plain

                second plain
                --b
                Content-Type: text/html

                <p>first html</p>
                --b
                Content-Type: text/html

                <p>second html</p>
                --b--
            """.trimIndent()

            assertEquals("first plain" to "<p>first html</p>", parser.extractPartsFromMultipart(content, "b"))

        },
        {
            val content = """
                --b

                conventional plain body
                --b--
            """.trimIndent()

            assertEquals("conventional plain body" to "", parser.extractPartsFromMultipart(content, "b"))

        },
        {
            val content = """
                --b
                Content-Type: application/octet-stream

                first bytes
                --b
                Content-Type: application/json

                {"second":true}
                --b--
            """.trimIndent()

            assertEquals("first bytes" to "", parser.extractPartsFromMultipart(content, "b"))

        },
        {
            val raw = """
                * 1 FETCH (BODY[TEXT] {11}
                hello world
                )
                A001 OK FETCH completed
            """.trimIndent()

            assertEquals("hello world" to "", parser.extractBodyFromResponse(raw))

        },
        {
            val raw = """
                * 1 FETCH (BODY.PEEK[TEXT]<0.200> {7}
                preview
                )
                A002 OK done
            """.trimIndent()

            assertEquals("preview" to "", parser.extractBodyFromResponse(raw))

        },
        {
            val raw = "headers\nmore headers\n\nfallback body\n)\nA003 OK done"

            assertEquals("fallback body", parser.extractFallbackBody(raw))
            assertEquals("fallback body" to "", parser.extractBodyFromResponse(raw))

        },
        {
            assertEquals("", parser.extractFallbackBody("no separator"))
            assertEquals("" to "", parser.extractBodyFromResponse("no separator"))

        },
        {
            val html = """
                <style>.secret { color: red }</style>
                <script>alert('secret')</script>
                <h1>Title</h1><p>Body</p>
            """.trimIndent()

            val stripped = parser.stripHtml(html)

            assertEquals("Title Body", stripped)
            assertFalse("secret" in stripped)
            assertFalse("alert" in stripped)

        },
        {
            val html = "<p>A&nbsp;&nbsp;B &amp; C &lt; D</p>\n<div>next\tline</div>"

            assertEquals("A B & C < D next line", parser.stripHtml(html))

        },
        {
            val raw = """
                * 1 FETCH (BODY[HEADER.FIELDS] {200}
                From: Alice <alice@example.com>
                To: Bob <bob@example.com>
                Subject: Quarterly report
                Date: Fri, 11 Jul 2026 09:30:00 +0900
                Message-ID: <m1@example.com>
                List-Unsubscribe: <https://example.com/unsubscribe?id=1>,
                 <mailto:unsubscribe@example.com>
                List-Unsubscribe-Post: List-Unsubscribe=One-Click
                BODY[TEXT] {5}
                hello
                )
                A001 OK done
            """.trimIndent()

            val message = parser.parseEmailFromFetch(42, "account", raw)!!

            assertEquals(42L, message.uid)
            assertEquals("account", message.accountId)
            assertEquals("Alice <alice@example.com>", message.from)
            assertEquals("Bob <bob@example.com>", message.to)
            assertEquals("Quarterly report", message.subject)
            assertEquals("<m1@example.com>", message.messageId)
            assertEquals("<https://example.com/unsubscribe?id=1>, <mailto:unsubscribe@example.com>", message.listUnsubscribe)
            assertEquals("List-Unsubscribe=One-Click", message.listUnsubscribePost)

        },
        {
            val raw = """
                fRoM: sender@example.com
                TO: receiver@example.com
                sUbJeCt: Mixed case
                dAtE: today
                mEsSaGe-Id: <mixed>
                BODY[TEXT] {4}
                body
                )
                A001 OK done
            """.trimIndent()

            val message = parser.parseEmailFromFetch(1, "a", raw)!!

            assertEquals("sender@example.com", message.from)
            assertEquals("receiver@example.com", message.to)
            assertEquals("Mixed case", message.subject)
            assertEquals("today", message.date)
            assertEquals("<mixed>", message.messageId)

        },
        {
            val message = parser.parseEmailFromFetch(
                uid = 7,
                accountId = "a",
                raw = "* 7 FETCH (BODY[TEXT] {4}\nbody\n)\nA001 OK done",
            )!!

            assertEquals("", message.from)
            assertEquals("", message.to)
            assertEquals("", message.subject)
            assertEquals("", message.date)
            assertEquals("", message.messageId)

        },
        {
            val raw = """
                Subject: first
                Subject: corrected
                BODY[TEXT] {4}
                body
                )
                A001 OK done
            """.trimIndent()

            assertEquals("corrected", parser.parseEmailFromFetch(1, "a", raw)?.subject)

        },
        {
            val raw = """
                From: Real Sender <real@example.com>
                Subject: Real subject
                BODY[TEXT] {70}
                From: Quoted Sender <quoted@example.com>
                Subject: Quoted subject
                )
                A001 OK done
            """.trimIndent()

            val message = parser.parseEmailFromFetch(1, "a", raw)!!

            assertEquals("Real Sender <real@example.com>", message.from)
            assertEquals("Real subject", message.subject)
            assertTrue("Quoted Sender" in message.body)

        },
        {
            val seen = parser.parseEmailFromFetch(1, "a", "FLAGS (\\Seen)\n\nbody")!!
            val unseen = parser.parseEmailFromFetch(2, "a", "FLAGS ()\n\nbody")!!

            assertTrue(seen.isRead)
            assertFalse(unseen.isRead)

        },
        {
            val lower = parser.parseEmailFromFetch(1, "a", "FLAGS (\\seen)\n\nbody")!!

            assertFalse(lower.isRead)

        },
        {
            val body = "a".repeat(150) + "\n" + "b".repeat(100)
            val raw = "BODY[TEXT] {251}\n$body\n)\nA001 OK done"

            val message = parser.parseEmailFromFetch(1, "a", raw)!!

            assertEquals(200, message.preview.length)
            assertFalse('\n' in message.preview)
            assertTrue(message.preview.startsWith("a".repeat(100)))

        },
        {
            val raw = """
                Subject: HTML only
                BODY[TEXT] {200}
                --b
                Content-Type: text/html

                <h1>Hello</h1><p>world &amp; team</p>
                --b--
                )
                A001 OK done
            """.trimIndent()

            val message = parser.parseEmailFromFetch(1, "a", raw)!!

            assertEquals("Hello world & team", message.body)
            assertEquals("<h1>Hello</h1><p>world &amp; team</p>", message.bodyHtml)
            assertEquals("Hello world & team", message.preview)

        },
        {
            val raw = """
                BODY[TEXT] {200}
                --b
                Content-Type: text/plain

                canonical plain
                --b
                Content-Type: text/html

                <p>alternate html</p>
                --b--
                )
                A001 OK done
            """.trimIndent()

            val message = parser.parseEmailFromFetch(1, "a", raw)!!

            assertEquals("canonical plain", message.body)
            assertEquals("<p>alternate html</p>", message.bodyHtml)

        },
    )

    @Test
    fun imapParserPreservesBodiesAndDecodesPartsWhenParsing() {
        imapParserCases().forEach { it() }
    }
}
