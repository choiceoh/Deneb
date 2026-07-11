package ai.deneb.email

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class ImapMessageParserTest {

    private val parser = ImapClient("mail.example")

    @Test
    fun escapeQuotedLeavesOrdinaryCredentialsUntouched() {
        assertEquals("user@example.com", parser.escapeQuoted("user@example.com"))
    }

    @Test
    fun escapeQuotedEscapesDoubleQuotes() {
        assertEquals("user\\\"name", parser.escapeQuoted("user\"name"))
    }

    @Test
    fun escapeQuotedEscapesBackslashes() {
        assertEquals("domain\\\\user", parser.escapeQuoted("domain\\user"))
    }

    @Test
    fun escapeQuotedHandlesAdjacentSlashAndQuoteWithoutDroppingEither() {
        assertEquals("a\\\\\\\"b", parser.escapeQuoted("a\\\"b"))
    }

    @Test
    fun quotedPrintablePassesPlainAsciiThrough() {
        assertEquals("plain text", parser.decodeQuotedPrintable("plain text"))
    }

    @Test
    fun quotedPrintableDecodesUpperAndLowerHexDigits() {
        assertEquals("ABC!", parser.decodeQuotedPrintable("=41=42=43=21"))
        assertEquals("z~", parser.decodeQuotedPrintable("=7a=7E"))
    }

    @Test
    fun quotedPrintableDecodesUtf8ByteSequences() {
        assertEquals("한글", parser.decodeQuotedPrintable("=ED=95=9C=EA=B8=80"))
    }

    @Test
    fun quotedPrintableRemovesLfSoftBreaks() {
        assertEquals("longline", parser.decodeQuotedPrintable("long=\nline"))
    }

    @Test
    fun quotedPrintableRemovesCrLfSoftBreaks() {
        assertEquals("longline", parser.decodeQuotedPrintable("long=\r\nline"))
    }

    @Test
    fun quotedPrintablePreservesMalformedHexEscapeVerbatim() {
        assertEquals("bad=GGvalue", parser.decodeQuotedPrintable("bad=GGvalue"))
    }

    @Test
    fun quotedPrintablePreservesTrailingIncompleteEscape() {
        assertEquals("tail=", parser.decodeQuotedPrintable("tail="))
        assertEquals("tail=A", parser.decodeQuotedPrintable("tail=A"))
    }

    @Test
    fun quotedPrintablePreservesRawUnicodeAlongsideEscapes() {
        assertEquals("한글!", parser.decodeQuotedPrintable("한글=21"))
    }

    @Test
    fun base64DecoderDecodesAscii() {
        assertEquals("Hello", parser.decodeBase64OrOriginal("SGVsbG8="))
    }

    @Test
    fun base64MimeDecoderAcceptsWrappedLinesAndWhitespace() {
        assertEquals("Hello world", parser.decodeBase64OrOriginal("SGVs\n bG8gd29ybGQ="))
    }

    @Test
    fun base64DecoderPreservesUtf8Text() {
        assertEquals("한글", parser.decodeBase64OrOriginal("7ZWc6riA"))
    }

    @Test
    fun invalidBase64FallsBackToOriginalPayload() {
        val original = "%%% not base64 %%%"

        assertEquals(original, parser.decodeBase64OrOriginal(original))
    }

    @Test
    fun emptyBase64PayloadDecodesToEmptyText() {
        assertEquals("", parser.decodeBase64OrOriginal(""))
    }

    @Test
    fun detectsConventionalMimeBoundaryLine() {
        assertEquals("boundary_123", parser.detectMimeBoundary("preamble\n--boundary_123\npart"))
    }

    @Test
    fun detectsBoundaryWithAllowedPunctuationAndSpaces() {
        assertEquals("mix+ed / value=?", parser.detectMimeBoundary("--mix+ed / value=?  \n"))
    }

    @Test
    fun ignoresTextThatIsNotAWholeBoundaryLine() {
        assertNull(parser.detectMimeBoundary("prefix --boundary suffix"))
        assertNull(parser.detectMimeBoundary("ordinary body"))
    }

    @Test
    fun extractsRawPartBodyAfterHeaderSeparator() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: 8bit\n\nbody\nline two"

        assertEquals("body\nline two", parser.extractPartBody(part))
    }

    @Test
    fun extractsAndDecodesQuotedPrintablePart() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: quoted-printable\n\nhello=20world=21"

        assertEquals("hello world!", parser.extractPartBody(part))
    }

    @Test
    fun extractsAndDecodesBase64Part() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: base64\n\n7ZWc6riA"

        assertEquals("한글", parser.extractPartBody(part))
    }

    @Test
    fun partWithoutBlankLineSkipsHeaderBlock() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: 8bit\nbody starts immediately"

        assertEquals("body starts immediately", parser.extractPartBody(part))
    }

    @Test
    fun unknownTransferEncodingLeavesPartBodyUntouched() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: x-custom\n\nraw=20body"

        assertEquals("raw=20body", parser.extractPartBody(part))
    }

    @Test
    fun malformedBase64PartRetainsOriginalBodyForDiagnostics() {
        val part = "Content-Type: text/plain\nContent-Transfer-Encoding: base64\n\n%%% broken %%%"

        assertEquals("%%% broken %%%", parser.extractPartBody(part))
    }

    @Test
    fun multipartExtractsPlainAndHtmlPartsIndependently() {
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
    }

    @Test
    fun multipartAllowsHtmlOnlyMessages() {
        val content = """
            --b
            Content-Type: text/html

            <strong>only html</strong>
            --b--
        """.trimIndent()

        assertEquals("" to "<strong>only html</strong>", parser.extractPartsFromMultipart(content, "b"))
    }

    @Test
    fun multipartUsesFirstPlainAndFirstHtmlPartWhenDuplicatesExist() {
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
    }

    @Test
    fun firstUntypedMultipartPartActsAsPlainText() {
        val content = """
            --b

            conventional plain body
            --b--
        """.trimIndent()

        assertEquals("conventional plain body" to "", parser.extractPartsFromMultipart(content, "b"))
    }

    @Test
    fun multipartFallsBackToFirstBodyWhenNoRecognizedContentTypeExists() {
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
    }

    @Test
    fun bodyMarkerExtractionRemovesClosingParenAndTaggedResponse() {
        val raw = """
            * 1 FETCH (BODY[TEXT] {11}
            hello world
            )
            A001 OK FETCH completed
        """.trimIndent()

        assertEquals("hello world" to "", parser.extractBodyFromResponse(raw))
    }

    @Test
    fun bodyPeekMarkerIsRecognized() {
        val raw = """
            * 1 FETCH (BODY.PEEK[TEXT]<0.200> {7}
            preview
            )
            A002 OK done
        """.trimIndent()

        assertEquals("preview" to "", parser.extractBodyFromResponse(raw))
    }

    @Test
    fun missingBodyMarkerFallsBackAfterFirstBlankLine() {
        val raw = "headers\nmore headers\n\nfallback body\n)\nA003 OK done"

        assertEquals("fallback body", parser.extractFallbackBody(raw))
        assertEquals("fallback body" to "", parser.extractBodyFromResponse(raw))
    }

    @Test
    fun missingMarkerAndSeparatorProduceEmptyBody() {
        assertEquals("", parser.extractFallbackBody("no separator"))
        assertEquals("" to "", parser.extractBodyFromResponse("no separator"))
    }

    @Test
    fun stripHtmlRemovesScriptsStylesAndTags() {
        val html = """
            <style>.secret { color: red }</style>
            <script>alert('secret')</script>
            <h1>Title</h1><p>Body</p>
        """.trimIndent()

        val stripped = parser.stripHtml(html)

        assertEquals("Title Body", stripped)
        assertFalse("secret" in stripped)
        assertFalse("alert" in stripped)
    }

    @Test
    fun stripHtmlDecodesEntitiesAndCollapsesWhitespace() {
        val html = "<p>A&nbsp;&nbsp;B &amp; C &lt; D</p>\n<div>next\tline</div>"

        assertEquals("A B & C < D next line", parser.stripHtml(html))
    }

    @Test
    fun parseFetchReadsAndUnfoldsHeaders() {
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
    }

    @Test
    fun headerNamesAreParsedCaseInsensitively() {
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
    }

    @Test
    fun absentHeadersRemainEmptyInsteadOfLeakingFetchMetadata() {
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
    }

    @Test
    fun laterDuplicateHeaderWinsWithinHeaderSection() {
        val raw = """
            Subject: first
            Subject: corrected
            BODY[TEXT] {4}
            body
            )
            A001 OK done
        """.trimIndent()

        assertEquals("corrected", parser.parseEmailFromFetch(1, "a", raw)?.subject)
    }

    @Test
    fun bodyLinesThatLookLikeHeadersDoNotOverrideEnvelopeHeaders() {
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
    }

    @Test
    fun seenFlagIsDetectedAnywhereInFetchResponse() {
        val seen = parser.parseEmailFromFetch(1, "a", "FLAGS (\\Seen)\n\nbody")!!
        val unseen = parser.parseEmailFromFetch(2, "a", "FLAGS ()\n\nbody")!!

        assertTrue(seen.isRead)
        assertFalse(unseen.isRead)
    }

    @Test
    fun seenFlagDetectionIsCaseSensitiveToImapSystemFlag() {
        val lower = parser.parseEmailFromFetch(1, "a", "FLAGS (\\seen)\n\nbody")!!

        assertFalse(lower.isRead)
    }

    @Test
    fun previewFlattensNewlinesAndCapsAtTwoHundredCharacters() {
        val body = "a".repeat(150) + "\n" + "b".repeat(100)
        val raw = "BODY[TEXT] {251}\n$body\n)\nA001 OK done"

        val message = parser.parseEmailFromFetch(1, "a", raw)!!

        assertEquals(200, message.preview.length)
        assertFalse('\n' in message.preview)
        assertTrue(message.preview.startsWith("a".repeat(100)))
    }

    @Test
    fun htmlOnlyFetchDerivesReadablePlainBodyAndKeepsOriginalHtml() {
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
    }

    @Test
    fun plainPartWinsForBodyWhileHtmlIsRetainedSeparately() {
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
    }
}
