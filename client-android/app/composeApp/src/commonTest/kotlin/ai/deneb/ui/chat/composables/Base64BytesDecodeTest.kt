package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertContentEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull

class Base64BytesDecodeTest {

    @Test
    fun decodesPaddedAsciiAndRejectsMissingPadding() {
        assertContentEquals("Hello".encodeToByteArray(), assertNotNull(decodeBase64BytesOrNull("SGVsbG8=")))
        assertNull(decodeBase64BytesOrNull("SGVsbG8"))
    }

    @Test
    fun decodesUtf8PayloadWithoutTextCorruption() {
        val decoded = assertNotNull(decodeBase64BytesOrNull("7ZWc6riA"))

        assertContentEquals("한글".encodeToByteArray(), decoded)
    }

    @Test
    fun decodesArbitraryBinaryIncludingZeroAndHighBits() {
        val decoded = assertNotNull(decodeBase64BytesOrNull("AAEC/w=="))

        assertContentEquals(byteArrayOf(0, 1, 2, -1), decoded)
    }

    @Test
    fun paddingVariantsCoverOneTwoAndThreeByteGroups() {
        assertContentEquals(byteArrayOf(0), assertNotNull(decodeBase64BytesOrNull("AA==")))
        assertContentEquals(byteArrayOf(0, 1), assertNotNull(decodeBase64BytesOrNull("AAE=")))
        assertContentEquals(byteArrayOf(0, 1, 2), assertNotNull(decodeBase64BytesOrNull("AAEC")))
        assertNull(decodeBase64BytesOrNull("AA"))
        assertNull(decodeBase64BytesOrNull("AAE"))
    }

    @Test
    fun emptyPayloadDecodesToEmptyByteArray() {
        assertContentEquals(byteArrayOf(), assertNotNull(decodeBase64BytesOrNull("")))
    }

    @Test
    fun malformedAlphabetAndPaddingReturnNull() {
        for (data in listOf("%%%", "SGV=sbG8=", "====", "A", "SGVsbG8===", "한글")) {
            assertNull(decodeBase64BytesOrNull(data), data)
        }
    }

    @Test
    fun defaultDecoderDoesNotSilentlyAcceptWhitespaceOrUrlAlphabet() {
        assertNull(decodeBase64BytesOrNull("SGVs bG8="))
        assertNull(decodeBase64BytesOrNull("SGVs\nbG8="))
        assertNull(decodeBase64BytesOrNull("_w=="))
    }

    @Test
    fun repeatedDecodeReturnsEquivalentIndependentArrays() {
        val first = assertNotNull(decodeBase64BytesOrNull("AAEC/w=="))
        val second = assertNotNull(decodeBase64BytesOrNull("AAEC/w=="))

        assertContentEquals(first, second)
        first[0] = 99
        assertContentEquals(byteArrayOf(0, 1, 2, -1), second)
    }
}
