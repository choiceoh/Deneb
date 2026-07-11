package ai.deneb.data

import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.withContext
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNull

class ConversationIdContextTest {

    @Test
    fun missingContextReturnsNull() = runTest {
        assertNull(currentConversationIdOrNull())
    }

    @Test
    fun contextElementExposesItsConversationId() = runTest {
        val actual = withContext(ConversationIdElement("client:main:abc")) {
            currentConversationIdOrNull()
        }

        assertEquals("client:main:abc", actual)
    }

    @Test
    fun idsArePreservedVerbatimIncludingBlankAndWhitespace() = runTest {
        for (id in listOf("", "   ", " chat:one ", "cron:job:123")) {
            val actual = withContext(ConversationIdElement(id)) { currentConversationIdOrNull() }
            assertEquals(id, actual)
        }
    }

    @Test
    fun nestedElementOverridesThenRestoresOuterId() = runTest {
        withContext(ConversationIdElement("outer")) {
            assertEquals("outer", currentConversationIdOrNull())

            withContext(ConversationIdElement("inner")) {
                assertEquals("inner", currentConversationIdOrNull())
            }

            assertEquals("outer", currentConversationIdOrNull())
        }
        assertNull(currentConversationIdOrNull())
    }

    @Test
    fun childCoroutineInheritsConversationId() = runTest {
        val inherited = withContext(ConversationIdElement("parent")) {
            coroutineScope {
                async { currentConversationIdOrNull() }.await()
            }
        }

        assertEquals("parent", inherited)
    }

    @Test
    fun siblingCoroutinesKeepIndependentIds() = runTest {
        val ids = (0 until 20).map { "conversation-$it" }

        val observed = coroutineScope {
            ids.map { id ->
                async(ConversationIdElement(id)) {
                    repeat(3) { assertEquals(id, currentConversationIdOrNull()) }
                    currentConversationIdOrNull()
                }
            }.awaitAll()
        }

        assertEquals(ids, observed)
        assertNull(currentConversationIdOrNull())
    }

    @Test
    fun exceptionFromNestedContextDoesNotLeakItsId() = runTest {
        withContext(ConversationIdElement("outer")) {
            assertFailsWith<IllegalStateException> {
                withContext(ConversationIdElement("inner")) {
                    assertEquals("inner", currentConversationIdOrNull())
                    error("failed turn")
                }
            }

            assertEquals("outer", currentConversationIdOrNull())
        }
    }

    @Test
    fun elementKeyRetrievesOnlyConversationElement() = runTest {
        val element = ConversationIdElement("keyed")

        withContext(element) {
            assertEquals(element, kotlin.coroutines.coroutineContext[ConversationIdElement])
            assertEquals("keyed", currentConversationIdOrNull())
        }
    }
}
