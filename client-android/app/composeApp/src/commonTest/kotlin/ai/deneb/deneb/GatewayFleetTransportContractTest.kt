package ai.deneb.deneb

import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/** HTTP boundary coverage for the SparkFleet passthrough. */
@OptIn(ExperimentalCoroutinesApi::class)
class GatewayFleetTransportContractTest {
    @Test
    fun fleetStateUsesAuthenticatedGatewayPassthroughAndMapsNestedMetrics() = runTest {
        val f = gatewayClientFixture(token = "fleet-secret", url = "https://gateway.example/")
        f.transport.enqueueJson(
            """{
                "nodes":[{
                    "name":"spark-a",
                    "role":"inference",
                    "reachable":true,
                    "error":null,
                    "metrics":{
                        "gpus":[{"index":0,"utilPct":97,"tempC":71}],
                        "memory":{"totalKB":134217728,"availableKB":67108864},
                        "disks":[{"path":"/models","totalKB":1000,"usedKB":640,"usePct":64}],
                        "services":[{"name":"vllm","ok":true}]
                    },
                    "models":[{"name":"Qwen/Model","sizeBytes":9223372036854775807}]
                }]
            }
            """.trimIndent(),
        )

        val state = f.client.fleetState()

        val request = f.transport.singleRequest()
        assertEquals("GET", request.method.value)
        assertEquals("https://gateway.example/api/v1/fleet/api/state", request.url)
        assertEquals("fleet-secret", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        val node = assertNotNull(state).nodes.single()
        assertEquals("spark-a", node.name)
        assertEquals("inference", node.role)
        assertTrue(node.reachable)
        assertNull(node.error)
        assertEquals(97, node.metrics.gpus.single().utilPct)
        assertEquals(71, node.metrics.gpus.single().tempC)
        assertEquals(134_217_728L, node.metrics.memory?.totalKB)
        assertEquals(67_108_864L, node.metrics.memory?.availableKB)
        assertEquals("/models", node.metrics.disks.single().path)
        assertEquals(64, node.metrics.disks.single().usePct)
        assertEquals("vllm", node.metrics.services.single().name)
        assertTrue(node.metrics.services.single().ok)
        assertEquals(Long.MAX_VALUE, node.models.single().sizeBytes)
    }

    @Test
    fun fleetStateCoercesGoNullCollectionsToSafeDefaults() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "nodes":[{
                    "name":"offline",
                    "reachable":false,
                    "error":"dial timeout",
                    "metrics":{
                        "gpus":null,
                        "memory":null,
                        "disks":null,
                        "services":null
                    },
                    "models":null
                }]
            }
            """.trimIndent(),
        )

        val node = assertNotNull(f.client.fleetState()).nodes.single()

        assertEquals("offline", node.name)
        assertFalse(node.reachable)
        assertEquals("dial timeout", node.error)
        assertTrue(node.metrics.gpus.isEmpty())
        assertNull(node.metrics.memory)
        assertTrue(node.metrics.disks.isEmpty())
        assertTrue(node.metrics.services.isEmpty())
        assertTrue(node.models.isEmpty())
    }

    @Test
    fun fleetStateIgnoresFutureFieldsAtEveryLevel() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "traceId":"trace-7",
                "nodes":[{
                    "name":"future-node",
                    "region":"kr-south",
                    "metrics":{"gpus":[],"powerWatts":412},
                    "models":[],
                    "future":{"nested":true}
                }]
            }
            """.trimIndent(),
        )

        val state = f.client.fleetState()

        assertEquals("future-node", state?.nodes?.single()?.name)
        assertTrue(state?.nodes?.single()?.metrics?.gpus?.isEmpty() == true)
    }

    @Test
    fun fleetStateReturnsNullForMalformedJson() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("not-json")

        val state = f.client.fleetState()

        assertNull(state)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun fleetStateReturnsNullForWrongRootShape() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("[]")

        val state = f.client.fleetState()

        assertNull(state)
    }

    @Test
    fun fleetStateReturnsNullForHttpFailureEvenWhenBodyLooksValid() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = """{"nodes":[{"name":"stale"}]}""",
            status = HttpStatusCode.ServiceUnavailable,
        )

        val state = f.client.fleetState()

        assertNull(state)
    }

    @Test
    fun fleetReadSkipsNetworkWithoutToken() = runTest {
        // Re-targeted at the surviving reads when the recipe/eval client left
        // (2026-08-28, recipes went AI-only): the no-credential contract is a
        // transport invariant, not a per-endpoint one.
        val f = gatewayClientFixture(token = "")

        assertNull(f.client.fleetState())
        assertNull(f.client.fleetJobs())
        assertEquals(0, f.transport.requests.size)
    }

    @Test
    fun fleetReadPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel fleet read"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fleetState()
        }

        assertEquals("cancel fleet read", failure.message)
    }

    @Test
    fun fleetReadDropsResponseWhenCredentialsChangeInFlight() = runTest {
        val f = gatewayClientFixture(token = "old-token", url = "https://old.example")
        val gate = CompletableDeferred<Unit>()
        f.transport.enqueueJson(
            body = """{"nodes":[{"name":"private-old-node"}]}""",
            gate = gate,
        )
        val pending = async { f.client.fleetState() }
        runCurrent()

        f.client.onCredentialsChanged("https://new.example", "new-token")
        gate.complete(Unit)

        assertNull(pending.await())
        val request = f.transport.singleRequest()
        assertEquals("https://old.example/api/v1/fleet/api/state", request.url)
        assertEquals("old-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
    }

    @Test
    fun fleetJobsMapsRunningDoneAndFailedRecordsWithoutReordering() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """[
                {"id":"job-running","title":"Launch","state":"running","log":"pulling","startedAt":"t1"},
                {"id":"job-done","title":"Eval","state":"done","log":"ok","startedAt":"t2","endedAt":"t3"},
                {"id":"job-failed","title":"Pull","state":"failed","log":"disk full","startedAt":"t4","endedAt":"t5"}
            ]
            """.trimIndent(),
        )

        val jobs = assertNotNull(f.client.fleetJobs())

        assertEquals(listOf("job-running", "job-done", "job-failed"), jobs.map { it.id })
        assertEquals(listOf("running", "done", "failed"), jobs.map { it.state })
        assertEquals("disk full", jobs.last().log)
        assertEquals("t5", jobs.last().endedAt)
    }

    @Test
    fun fleetJobEncodesOpaqueIdAsSinglePathSegment() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{"id":"job /?#한글","title":"One","state":"done"}""",
        )

        val job = f.client.fleetJob("job /?#한글")

        assertEquals("job /?#한글", job?.id)
        assertEquals(
            "https://gateway.example/api/v1/fleet/api/jobs/job%20%2F%3F%23%ED%95%9C%EA%B8%80",
            f.transport.singleRequest().url,
        )
    }

    @Test
    fun fleetJobReturnsNullWhenRequiredIdIsMissing() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"title":"invalid","state":"done"}""")

        val job = f.client.fleetJob("requested")

        assertNull(job)
    }

    @Test
    fun fleetHFSearchEncodesQueryAndOptionalSort() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "models":[{
                    "id":"org/model",
                    "downloads":123456789,
                    "likes":777,
                    "params":72000000000,
                    "pipelineTag":"text-generation",
                    "lastModified":"2026-07-10T00:00:00Z",
                    "gated":true
                }]
            }
            """.trimIndent(),
        )

        val models = f.client.fleetHFSearch("Qwen 3 / coder?", sort = "lastModified")

        assertEquals(
            "https://gateway.example/api/v1/fleet/api/hf/search?q=Qwen%203%20%2F%20coder%3F&sort=lastModified",
            f.transport.singleRequest().url,
        )
        val model = assertNotNull(models).single()
        assertEquals("org/model", model.id)
        assertEquals(123_456_789L, model.downloads)
        assertEquals(777L, model.likes)
        assertEquals(72_000_000_000L, model.params)
        assertEquals("text-generation", model.pipelineTag)
        assertEquals("2026-07-10T00:00:00Z", model.lastModified)
        assertTrue(model.gated)
    }

    @Test
    fun fleetHFSearchOmitsSortDelimiterWhenSortBlank() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"models":[]}""")

        val models = f.client.fleetHFSearch("model")

        assertNotNull(models)
        assertTrue(models.isEmpty())
        assertEquals(
            "https://gateway.example/api/v1/fleet/api/hf/search?q=model",
            f.transport.singleRequest().url,
        )
    }

    @Test
    fun fleetHFSearchReturnsNullForWrongModelShape() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"models":[{"downloads":7}]}""")

        val models = f.client.fleetHFSearch("broken")

        assertNull(models)
    }

    @Test
    fun fleetHFInfoEncodesRepoAndMapsLargeMetadata() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "repo":"org/model-gguf",
                "sizeBytes":9223372036854775807,
                "files":2147483647,
                "gated":false
            }
            """.trimIndent(),
        )

        val info = f.client.fleetHFInfo("org/model gguf")

        assertEquals(
            "https://gateway.example/api/v1/fleet/api/hf/info?repo=org%2Fmodel%20gguf",
            f.transport.singleRequest().url,
        )
        assertEquals("org/model-gguf", info?.repo)
        assertEquals(Long.MAX_VALUE, info?.sizeBytes)
        assertEquals(Int.MAX_VALUE, info?.files)
        assertFalse(info?.gated ?: true)
    }

    @Test
    fun fleetDownloadModelPostsNodeAndRepoAndForwardsJob() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"jobId":"download-42"}""")
        val jobs = mutableListOf<String>()

        val error = f.client.fleetDownloadModel(
            node = "spark-b",
            repo = "org/private-model",
            onJob = jobs::add,
        )

        assertNull(error)
        val request = f.transport.singleRequest()
        assertEquals("https://gateway.example/api/v1/fleet/api/models/download", request.url)
        assertEquals("spark-b", request.jsonBody?.get("node")?.jsonPrimitive?.content)
        assertEquals("org/private-model", request.jsonBody?.get("repo")?.jsonPrimitive?.content)
        assertEquals(listOf("download-42"), jobs)
    }

    @Test
    fun fleetDownloadModelSurfacesRejectedGatedRepository() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "Hugging Face access approval required",
            status = HttpStatusCode.Forbidden,
        )

        val error = f.client.fleetDownloadModel("spark", "gated/model")

        assertEquals("Hugging Face access approval required", error)
    }

    @Test
    fun fleetCancelJobEncodesIdAndPostsEmptyJsonObject() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("{}")

        val error = f.client.fleetCancelJob("job /?#7")

        assertNull(error)
        val request = f.transport.singleRequest()
        assertEquals("POST", request.method.value)
        assertEquals(
            "https://gateway.example/api/v1/fleet/api/jobs/job%20%2F%3F%237/cancel",
            request.url,
        )
        assertEquals(JsonObject(emptyMap()), request.jsonBody)
    }

    @Test
    fun fleetCancelJobSurfacesAlreadyFinishedConflict() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "job is already finished",
            status = HttpStatusCode.Conflict,
        )

        val error = f.client.fleetCancelJob("done-job")

        assertEquals("job is already finished", error)
    }
}
