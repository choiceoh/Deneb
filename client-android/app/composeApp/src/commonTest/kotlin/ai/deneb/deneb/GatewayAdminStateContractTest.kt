package ai.deneb.deneb

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** State-publication behavior for model, skill, cron, and client-status refreshes. */
class GatewayAdminStateContractTest {
    @Test
    fun refreshModelsFlattensSectionsFiltersBlankIdsDeduplicatesAndPublishesRoleMetadata() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "current":"provider/current",
                "roles":[
                    {"role":"main","model":"provider/current"},
                    {"role":"fallback","model":"provider/fallback"}
                ],
                "sections":[
                    {
                        "title":"Cloud",
                        "models":[
                            {"id":"","label":"drop"},
                            {
                                "id":"provider/current",
                                "label":"Current label",
                                "display":"Current display",
                                "health":"ok",
                                "custom":false,
                                "deletable":false,
                                "unhealthy":false,
                                "note":"primary"
                            },
                            {
                                "id":"provider/duplicate",
                                "label":"First duplicate",
                                "display":"",
                                "health":"degraded",
                                "custom":true,
                                "deletable":true,
                                "unhealthy":true,
                                "note":"first wins"
                            }
                        ]
                    },
                    {
                        "title":"Local",
                        "models":[
                            {"id":"provider/duplicate","label":"Second duplicate","display":"wrong"},
                            {"id":"provider/id-fallback","label":"","display":""}
                        ]
                    }
                ],
                "advisories":["provider/duplicate: 점검"],
                "mainHasVision":true
            }
            """.trimIndent(),
        )

        val refreshed = f.client.refreshModels()

        assertTrue(refreshed)
        assertEquals("miniapp.models.list", f.transport.singleRequest().rpcMethod)
        val models = f.client.denebModels.value
        assertEquals(
            listOf("provider/current", "provider/duplicate", "provider/id-fallback"),
            models.map { it.id },
        )
        assertEquals(
            listOf("Current display", "First duplicate", "provider/id-fallback"),
            models.map { it.display },
        )
        assertEquals(listOf(true, false, false), models.map { it.current })
        assertEquals("degraded", models[1].health)
        assertTrue(models[1].custom)
        assertTrue(models[1].deletable)
        assertTrue(models[1].unhealthy)
        assertEquals("first wins", models[1].note)
        assertEquals(
            mapOf(
                "main" to "provider/current",
                "fallback" to "provider/fallback",
            ),
            f.client.denebRoleModels.value,
        )
        assertEquals(listOf("provider/duplicate: 점검"), f.client.denebModelAdvisories.value)
        assertTrue(f.client.denebMainHasVision.value)
    }

    @Test
    fun failedModelRefreshPreservesPublishedRegistryAndMetadata() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "current":"model",
                "roles":[{"role":"main","model":"model"}],
                "sections":[{"models":[{"id":"model","display":"Stable"}]}],
                "advisories":["stable"],
                "mainHasVision":true
            }
            """.trimIndent(),
        )
        assertTrue(f.client.refreshModels())
        f.transport.enqueueJson("not-json")

        val refreshed = f.client.refreshModels()

        assertFalse(refreshed)
        assertEquals(listOf("model"), f.client.denebModels.value.map { it.id })
        assertEquals(mapOf("main" to "model"), f.client.denebRoleModels.value)
        assertEquals(listOf("stable"), f.client.denebModelAdvisories.value)
        assertTrue(f.client.denebMainHasVision.value)
    }

    @Test
    fun refreshSkillsPublishesGatewayOrderIncludingRichMetadata() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "skills":[
                    {
                        "name":"coding/github",
                        "description":"GitHub workflow",
                        "category":"coding",
                        "homepage":"https://example.com",
                        "tags":["git","pr"],
                        "relatedSkills":["coding/review"],
                        "source":"workspace",
                        "version":"2",
                        "origin":"local",
                        "createdAt":100,
                        "evolveCount":3,
                        "lastEvolvedAt":200,
                        "totalUses":17,
                        "lastUsedAt":300,
                        "curatorState":"active",
                        "editable":true,
                        "deletable":true,
                        "dependencySummary":["gh"],
                        "installSummary":["ready"]
                    },
                    {"name":"knowledge/interview","description":"Interview","category":"knowledge"}
                ],
                "count":2
            }
            """.trimIndent(),
        )

        val refreshed = f.client.refreshSkills()

        assertTrue(refreshed)
        assertEquals(listOf("coding/github", "knowledge/interview"), f.client.denebSkills.value.map { it.name })
        val skill = f.client.denebSkills.value.first()
        assertEquals("GitHub workflow", skill.description)
        assertEquals("coding", skill.category)
        assertEquals(listOf("git", "pr"), skill.tags)
        assertEquals(listOf("coding/review"), skill.relatedSkills)
        assertEquals("workspace", skill.source)
        assertEquals("2", skill.version)
        assertEquals(3, skill.evolveCount)
        assertEquals(17, skill.totalUses)
        assertTrue(skill.editable)
        assertTrue(skill.deletable)
        assertEquals(listOf("gh"), skill.dependencySummary)
        assertEquals(listOf("ready"), skill.installSummary)
    }

    @Test
    fun failedSkillRefreshPreservesPriorRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"skills":[{"name":"stable"}],"count":1}""")
        f.client.refreshSkills()
        f.transport.enqueueJson("not-json")

        val refreshed = f.client.refreshSkills()

        assertFalse(refreshed)
        assertEquals(listOf("stable"), f.client.denebSkills.value.map { it.name })
    }

    @Test
    fun refreshScheduledTasksFiltersBlankIdsKeepsLastDuplicateAndMapsFailureState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "jobs":[
                    {"id":"","name":"drop"},
                    {"id":"cron-1","name":"Old","schedule":"old","payloadPreview":"old","nextRunAtMs":1},
                    {
                        "id":"cron-2",
                        "name":"",
                        "schedule":"0 8 * * *",
                        "payloadPreview":"Morning brief",
                        "nextRunAtMs":9223372036854775807,
                        "consecutiveErrors":4,
                        "lastError":"delivery failed"
                    },
                    {
                        "id":"cron-1",
                        "name":"New",
                        "schedule":"0 9 * * *",
                        "payloadPreview":"Daily brief",
                        "nextRunAtMs":42,
                        "consecutiveErrors":0,
                        "lastError":""
                    }
                ]
            }
            """.trimIndent(),
        )

        val refreshed = f.client.refreshScheduledTasks()

        assertTrue(refreshed)
        assertEquals(true, f.transport.singleRequest().rpcParams?.get("includeDisabled")?.toString()?.toBoolean())
        val tasks = f.client.denebScheduledTasks.value
        assertEquals(listOf("cron-2", "cron-1"), tasks.map { it.id })
        assertEquals(listOf("cron-2", "New"), tasks.map { it.description })
        assertEquals(listOf("Morning brief", "Daily brief"), tasks.map { it.prompt })
        assertEquals(listOf(Long.MAX_VALUE, 42L), tasks.map { it.scheduledAtEpochMs })
        assertEquals(listOf("0 8 * * *", "0 9 * * *"), tasks.map { it.cron })
        assertEquals(listOf(4, 0), tasks.map { it.consecutiveFailures })
        assertEquals(listOf("delivery failed", null), tasks.map { it.lastResult })
        assertTrue(tasks.all { it.createdAtEpochMs == 0L })
    }

    @Test
    fun failedScheduledTaskRefreshPreservesPriorSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"jobs":[{"id":"stable","name":"Stable"}]}""")
        f.client.refreshScheduledTasks()
        f.transport.enqueueJson("not-json")

        val refreshed = f.client.refreshScheduledTasks()

        assertFalse(refreshed)
        assertEquals(listOf("stable"), f.client.denebScheduledTasks.value.map { it.id })
    }

    @Test
    fun refreshClientStatusMapsCapabilitiesEndpointsAndTimestamp() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{
                "version":"2026.7",
                "nativeApiVersion":3,
                "model":"provider/model",
                "capabilities":{"chat":true,"files":true,"mail":false},
                "endpoints":{"rpc":"/api/v1/miniapp/rpc","events":"/api/v1/miniapp/events"},
                "tsMs":9223372036854775807
            }
            """.trimIndent(),
        )

        val status = f.client.refreshClientStatus()

        assertEquals("miniapp.client.hello", f.transport.singleRequest().rpcMethod)
        assertEquals("2026.7", status?.version)
        assertEquals(3, status?.nativeApiVersion)
        assertEquals("provider/model", status?.model)
        assertEquals(mapOf("chat" to true, "files" to true, "mail" to false), status?.capabilities)
        assertEquals(
            mapOf(
                "rpc" to "/api/v1/miniapp/rpc",
                "events" to "/api/v1/miniapp/events",
            ),
            status?.endpoints,
        )
        assertEquals(Long.MAX_VALUE, status?.timestampMs)
        assertEquals(status, f.client.clientStatus.value)
    }

    @Test
    fun failedClientStatusRefreshExplicitlyClearsStaleStatus() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"version":"stable"}""")
        f.client.refreshClientStatus()
        f.transport.enqueueJson("not-json")

        val status = f.client.refreshClientStatus()

        assertNull(status)
        assertNull(f.client.clientStatus.value)
    }

    @Test
    fun administrativeCancellationPropagatesWithoutReplacingModelState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            """{"current":"stable","sections":[{"models":[{"id":"stable"}]}]}""",
        )
        f.client.refreshModels()
        f.transport.enqueueFailure(CancellationException("cancel models"))

        val failure = assertFailsWith<CancellationException> {
            f.client.refreshModels()
        }

        assertEquals("cancel models", failure.message)
        assertEquals(listOf("stable"), f.client.denebModels.value.map { it.id })
    }
}
