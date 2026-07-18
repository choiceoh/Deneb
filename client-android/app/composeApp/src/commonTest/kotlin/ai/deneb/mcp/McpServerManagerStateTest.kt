package ai.deneb.mcp

import ai.deneb.data.AppSettings
import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class McpServerManagerStateTest {

    private data class Fixture(
        val settings: AppSettings,
        val manager: McpServerManager,
    )

    private fun fixture(): Fixture {
        val settings = AppSettings(MapSettings())
        return Fixture(settings, McpServerManager(settings))
    }

    @Test
    fun missingAndMalformedPersistenceReadAsEmpty() {
        val f = fixture()
        assertEquals(emptyList(), f.manager.getServers())

        for (raw in listOf("broken", "{}", "[1,2]")) {
            f.settings.setMcpServersJson(raw)
            assertEquals(emptyList(), f.manager.getServers(), raw)
        }
    }

    @Test
    fun addServerPreservesConnectionFieldsAndPersists() {
        val f = fixture()
        val added = f.manager.addServer(
            name = "Work Tools",
            url = "https://mcp.example.test/rpc",
            headers = mapOf("Authorization" to "Bearer token", "X-Tenant" to "deneb"),
        )

        assertEquals("work_tools", added.id)
        assertEquals("Work Tools", added.name)
        assertEquals("https://mcp.example.test/rpc", added.url)
        assertEquals("Bearer token", added.headers["Authorization"])
        assertTrue(added.isEnabled)
        assertEquals(listOf(added), f.manager.getServers())
        assertEquals(listOf(added), McpServerManager(f.settings).getServers())
    }

    @Test
    fun generatedIdLowercasesAndReplacesEveryNonAsciiToken() {
        val f = fixture()

        val added = f.manager.addServer("My Server! 2026", "url", emptyMap())

        assertEquals("my_server__2026", added.id)
    }

    @Test
    fun duplicateNamesReceiveFirstAvailableNumericSuffix() {
        val f = fixture()

        val first = f.manager.addServer("Tools", "one", emptyMap())
        val second = f.manager.addServer("Tools", "two", emptyMap())
        val third = f.manager.addServer("Tools", "three", emptyMap())

        assertEquals(listOf("tools", "tools_2", "tools_3"), listOf(first.id, second.id, third.id))
    }

    @Test
    fun differentlyWrittenNamesThatNormalizeTogetherShareSuffixSequence() {
        val f = fixture()

        val ids = listOf("A B", "a-b", "a_b", "A.B").map { name ->
            f.manager.addServer(name, name, emptyMap()).id
        }

        assertEquals(listOf("a_b", "a_b_2", "a_b_3", "a_b_4"), ids)
    }

    @Test
    fun generatedBaseIdIsCappedAtThirtyCharacters() {
        val f = fixture()
        val name = "abcdefghij".repeat(4) + "-extra"

        val added = f.manager.addServer(name, "url", emptyMap())

        assertEquals(30, added.id.length)
        assertEquals(name.lowercase().take(30), added.id)
    }

    @Test
    fun namesCollidingAfterThirtyCharacterCapStillRemainUnique() {
        val f = fixture()
        val shared = "a".repeat(30)

        val first = f.manager.addServer(shared + "first", "one", emptyMap())
        val second = f.manager.addServer(shared + "second", "two", emptyMap())

        assertEquals(shared, first.id)
        assertEquals("${shared}_2", second.id)
        assertTrue(second.id.length > 30)
    }

    @Test
    fun blankOrSymbolOnlyNameStillGetsUsableStableId() {
        val f = fixture()

        val blank = f.manager.addServer("", "one", emptyMap())
        val symbols = f.manager.addServer("!!!", "two", emptyMap())
        val anotherBlank = f.manager.addServer("", "three", emptyMap())

        assertEquals("server", blank.id)
        assertEquals("___", symbols.id)
        assertEquals("server_2", anotherBlank.id)
        assertTrue(f.manager.getServers().all { it.id.isNotBlank() })
    }

    @Test
    fun disablingServerUpdatesOnlyTargetAndCanBeReversed() {
        val f = fixture()
        val first = f.manager.addServer("First", "one", emptyMap())
        val second = f.manager.addServer("Second", "two", emptyMap())

        f.manager.setServerEnabled(first.id, false)

        assertFalse(f.manager.getServers().first { it.id == first.id }.isEnabled)
        assertTrue(f.manager.getServers().first { it.id == second.id }.isEnabled)

        f.manager.setServerEnabled(first.id, true)
        assertTrue(f.manager.getServers().first { it.id == first.id }.isEnabled)
    }

    @Test
    fun enablingUnknownIdDoesNotRewriteServerSet() {
        val f = fixture()
        f.manager.addServer("Known", "url", emptyMap())
        val before = f.settings.getMcpServersJson()

        f.manager.setServerEnabled("missing", false)

        assertEquals(before, f.settings.getMcpServersJson())
        assertEquals(listOf("known"), f.manager.getServers().map { it.id })
    }

    @Test
    fun removeServerDeletesOnlyMatchingConfiguration() {
        val f = fixture()
        val first = f.manager.addServer("First", "one", emptyMap())
        val second = f.manager.addServer("Second", "two", emptyMap())

        f.manager.removeServer(first.id)

        assertEquals(listOf(second), f.manager.getServers())
        assertEquals(listOf(second), McpServerManager(f.settings).getServers())
    }

    @Test
    fun removedBaseIdCanBeReusedWithoutUnnecessarySuffix() {
        val f = fixture()
        val first = f.manager.addServer("Reusable", "one", emptyMap())
        f.manager.addServer("Reusable", "two", emptyMap())

        f.manager.removeServer(first.id)
        val replacement = f.manager.addServer("Reusable", "three", emptyMap())

        assertEquals("reusable", replacement.id)
        assertEquals(setOf("reusable_2", "reusable"), f.manager.getServers().map { it.id }.toSet())
    }

    @Test
    fun managerObservesExternalPersistenceChangesAfterCachedRead() {
        val f = fixture()
        val original = f.manager.addServer("Original", "one", emptyMap())
        assertEquals(listOf(original), f.manager.getServers())
        val replacement = McpServerConfig("external", "External", "two", isEnabled = false)

        f.settings.setMcpServersJson(Json.encodeToString(listOf(replacement)))

        assertEquals(listOf(replacement), f.manager.getServers())
    }

    @Test
    fun externalMalformedReplacementDoesNotLeakPreviouslyCachedServers() {
        val f = fixture()
        f.manager.addServer("Cached", "one", emptyMap())
        assertEquals(1, f.manager.getServers().size)

        f.settings.setMcpServersJson("broken")

        assertEquals(emptyList(), f.manager.getServers())
    }

    @Test
    fun cachedMalformedReadDoesNotHideLaterExternalRecovery() {
        val f = fixture()
        f.settings.setMcpServersJson("broken")
        assertEquals(emptyList(), f.manager.getServers())
        val recovered = McpServerConfig("recovered", "Recovered", "url")

        f.settings.setMcpServersJson(Json.encodeToString(listOf(recovered)))

        assertEquals(listOf(recovered), f.manager.getServers())
    }

    @Test
    fun malformedPersistenceIsHealedByAddingServer() {
        val f = fixture()
        f.settings.setMcpServersJson("broken")

        val added = f.manager.addServer("Recovered", "url", emptyMap())

        assertEquals(listOf(added), f.manager.getServers())
        assertTrue(f.settings.getMcpServersJson().startsWith("["))
    }

    @Test
    fun undiscoveredServersExposeNoExecutableTools() {
        val f = fixture()
        val server = f.manager.addServer("Tools", "url", emptyMap())

        assertEquals(emptyList(), f.manager.getToolsForServer(server.id))
        assertEquals(emptyList(), f.manager.getEnabledMcpTools())
        assertFalse(f.manager.isConnected(server.id))
    }

    @Test
    fun connectMissingServerReturnsFailureWithoutConnectedState() = runTest {
        val f = fixture()

        val result = f.manager.connectAndDiscoverTools("missing")

        assertTrue(result.isFailure)
        assertEquals("Server not found: missing", result.exceptionOrNull()?.message)
        assertFalse(f.manager.isConnected("missing"))
        assertEquals(emptyList(), f.manager.getToolsForServer("missing"))
    }

    @Test
    fun connectEnabledServersWithEmptyStateCompletesWithoutMutation() = runTest {
        val f = fixture()

        f.manager.connectEnabledServers()

        assertEquals(emptyList(), f.manager.getServers())
        assertEquals(emptyList(), f.manager.getEnabledMcpTools())
        assertEquals("", f.settings.getMcpServersJson())
    }
}
