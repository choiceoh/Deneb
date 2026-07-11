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
        val f = gatewayClientFixture(token = "")

        val state = f.client.fleetState()
        val recipes = f.client.fleetRecipes()
        val jobs = f.client.fleetJobs()
        val evals = f.client.fleetEvals()

        assertNull(state)
        assertNull(recipes)
        assertNull(jobs)
        assertNull(evals)
        assertTrue(f.transport.requests.isEmpty())
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
    fun fleetRecipesMapsStatusAndOptionalVllmLimits() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """[{
                "name":"qwen-72b",
                "description":"production reasoning",
                "node":"spark-a",
                "container":"vllm-qwen",
                "port":8000,
                "vllm":{
                    "gpuMemoryUtilization":0.91,
                    "maxModelLen":32768,
                    "maxNumSeqs":8
                },
                "status":{
                    "running":true,
                    "weightsPresent":true,
                    "node":"spark-a"
                }
            }]
            """.trimIndent(),
        )

        val recipe = assertNotNull(f.client.fleetRecipes()).single()

        assertEquals("https://gateway.example/api/v1/fleet/api/recipes", f.transport.singleRequest().url)
        assertEquals("qwen-72b", recipe.name)
        assertEquals("production reasoning", recipe.description)
        assertEquals("spark-a", recipe.node)
        assertEquals("vllm-qwen", recipe.container)
        assertEquals(8000, recipe.port)
        assertEquals(0.91, recipe.vllm?.gpuMemoryUtilization)
        assertEquals(32768, recipe.vllm?.maxModelLen)
        assertEquals(8, recipe.vllm?.maxNumSeqs)
        assertTrue(recipe.status.running)
        assertTrue(recipe.status.weightsPresent)
    }

    @Test
    fun fleetRecipesReturnsValidEmptyListDistinctFromFailure() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("[]")

        val recipes = f.client.fleetRecipes()

        assertNotNull(recipes)
        assertTrue(recipes.isEmpty())
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
    fun fleetLaunchActionPostsRecipeActionAndForwardsJobId() = runTest {
        val f = gatewayClientFixture(token = "action-token")
        f.transport.enqueueJson("""{"jobId":"launch-7"}""")
        val jobs = mutableListOf<String>()

        val error = f.client.fleetRecipeAction(
            recipe = "qwen-72b",
            action = "launch",
            onJob = jobs::add,
        )

        assertNull(error)
        val request = f.transport.singleRequest()
        assertEquals("POST", request.method.value)
        assertEquals("https://gateway.example/api/v1/fleet/api/recipes/action", request.url)
        assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
        assertEquals("action-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
        assertEquals("qwen-72b", request.jsonBody?.get("recipe")?.jsonPrimitive?.content)
        assertEquals("launch", request.jsonBody?.get("action")?.jsonPrimitive?.content)
        assertFalse(request.jsonBody?.containsKey("overrides") ?: true)
        assertEquals(listOf("launch-7"), jobs)
    }

    @Test
    fun fleetLaunchActionIncludesOnlySpecifiedVllmOverrides() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"jobId":"tuned-1"}""")

        val error = f.client.fleetRecipeAction(
            recipe = "large-model",
            action = "launch",
            overrides = FleetVllm(
                gpuMemoryUtilization = 0.82,
                maxModelLen = null,
                maxNumSeqs = 3,
            ),
        )

        assertNull(error)
        val body = assertNotNull(f.transport.singleRequest().jsonBody)
        val overrides = body["overrides"] as JsonObject
        assertEquals(0.82, overrides["gpuMemoryUtilization"]?.jsonPrimitive?.content?.toDouble())
        assertEquals(3, overrides["maxNumSeqs"]?.jsonPrimitive?.content?.toInt())
        assertFalse(overrides.containsKey("maxModelLen"))
        assertEquals(2, overrides.size)
    }

    @Test
    fun fleetNonLaunchActionNeverLeaksLaunchOverrides() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("{}")

        val error = f.client.fleetRecipeAction(
            recipe = "qwen",
            action = "restart",
            overrides = FleetVllm(
                gpuMemoryUtilization = 0.1,
                maxModelLen = 1,
                maxNumSeqs = 1,
            ),
        )

        assertNull(error)
        val body = assertNotNull(f.transport.singleRequest().jsonBody)
        assertEquals("restart", body["action"]?.jsonPrimitive?.content)
        assertFalse(body.containsKey("overrides"))
    }

    @Test
    fun fleetLaunchWithAllNullOverridesOmitsEmptyObject() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("{}")

        f.client.fleetRecipeAction(
            recipe = "qwen",
            action = "launch",
            overrides = FleetVllm(),
        )

        assertFalse(f.transport.singleRequest().jsonBody?.containsKey("overrides") ?: true)
    }

    @Test
    fun fleetActionTreatsMalformedSuccessBodyAsAcceptedWithoutFakeJob() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("not-json")
        val jobs = mutableListOf<String>()

        val error = f.client.fleetRecipeAction("qwen", "stop", onJob = jobs::add)

        assertNull(error)
        assertTrue(jobs.isEmpty())
    }

    @Test
    fun fleetActionDoesNotEmitBlankJobId() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"jobId":"   "}""")
        val jobs = mutableListOf<String>()

        val error = f.client.fleetRecipeAction("qwen", "launch", onJob = jobs::add)

        assertNull(error)
        assertTrue(jobs.isEmpty())
    }

    @Test
    fun fleetActionSurfacesUpstreamErrorBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "insufficient GPU memory",
            status = HttpStatusCode.Conflict,
        )

        val error = f.client.fleetRecipeAction("oversized", "launch")

        assertEquals("insufficient GPU memory", error)
    }

    @Test
    fun fleetActionBuildsStatusFallbackForBlankErrorBody() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "",
            status = HttpStatusCode.TooManyRequests,
        )

        val error = f.client.fleetRecipeAction("busy", "launch")

        assertEquals("요청 실패 (429)", error)
    }

    @Test
    fun fleetActionReturnsConnectionMessageWithoutTokenAndSkipsNetwork() = runTest {
        val f = gatewayClientFixture(token = "")

        val error = f.client.fleetRecipeAction("qwen", "launch")

        assertEquals("게이트웨이에 연결되어 있지 않습니다.", error)
        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun fleetActionPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel fleet action"))

        val failure = assertFailsWith<CancellationException> {
            f.client.fleetRecipeAction("qwen", "launch")
        }

        assertEquals("cancel fleet action", failure.message)
    }

    @Test
    fun fleetActionSurfacesTransportFailureWithoutThrowing() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(IllegalStateException("connection reset"))

        val error = f.client.fleetRecipeAction("qwen", "launch")

        assertEquals("플릿에 연결하지 못했습니다: connection reset", error)
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

    @Test
    fun fleetDiagnosePostsOpaqueNodeAndContainerAndMapsFindings() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "container":"vllm-qwen",
                "state":"exited",
                "findings":[{
                    "cause":"CUDA out of memory",
                    "fix":"lower gpuMemoryUtilization",
                    "match":"OutOfMemoryError"
                }],
                "llm":"The KV cache exceeded the available budget."
            }
            """.trimIndent(),
        )

        val diagnosis = f.client.fleetDiagnose("spark-a", "vllm-qwen")

        val request = f.transport.singleRequest()
        assertEquals("https://gateway.example/api/v1/fleet/api/assist/logs", request.url)
        assertEquals("spark-a", request.jsonBody?.get("node")?.jsonPrimitive?.content)
        assertEquals("vllm-qwen", request.jsonBody?.get("container")?.jsonPrimitive?.content)
        assertEquals("exited", diagnosis?.state)
        assertEquals("The KV cache exceeded the available budget.", diagnosis?.llm)
        val finding = assertNotNull(diagnosis).findings.single()
        assertEquals("CUDA out of memory", finding.cause)
        assertEquals("lower gpuMemoryUtilization", finding.fix)
        assertEquals("OutOfMemoryError", finding.match)
    }

    @Test
    fun fleetDiagnoseReturnsNullForUpstreamFailureAndIgnoresErrorText() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "container not found",
            status = HttpStatusCode.NotFound,
        )

        val diagnosis = f.client.fleetDiagnose("spark", "missing")

        assertNull(diagnosis)
    }

    @Test
    fun fleetDriftEncodesRecipeAndNodeAndMapsDiffs() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "recipe":"qwen / prod",
                "node":"spark #1",
                "container":"vllm-qwen",
                "inSync":false,
                "diffs":["maxModelLen: expected 32768, actual 8192","gpuMemoryUtilization differs"]
            }
            """.trimIndent(),
        )

        val drift = f.client.fleetDrift("qwen / prod", "spark #1")

        assertEquals(
            "https://gateway.example/api/v1/fleet/api/recipes/qwen%20%2F%20prod/drift?node=spark%20%231",
            f.transport.singleRequest().url,
        )
        assertEquals("qwen / prod", drift?.recipe)
        assertEquals("spark #1", drift?.node)
        assertFalse(drift?.inSync ?: true)
        assertEquals(2, drift?.diffs?.size)
    }

    @Test
    fun fleetDriftDefaultsToInSyncWhenGatewayOmitsFlag() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"recipe":"r","node":"n","diffs":[]}""")

        val drift = f.client.fleetDrift("r", "n")

        assertTrue(drift?.inSync == true)
        assertTrue(drift?.diffs?.isEmpty() == true)
    }

    @Test
    fun fleetContainerLogsPreservesPlainTextAndEncodesQuery() = runTest {
        val f = gatewayClientFixture()
        val raw = "line 1\n한글 line 2\nANSI? [31merror[0m\n"
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = raw,
                contentType = "text/plain; charset=utf-8",
            ),
        )

        val logs = f.client.fleetContainerLogs("spark /1", "vllm #1", tail = 2147483647)

        assertEquals(raw, logs)
        assertEquals(
            "https://gateway.example/api/v1/fleet/api/logs?node=spark%20%2F1&container=vllm%20%231&tail=2147483647",
            f.transport.singleRequest().url,
        )
    }

    @Test
    fun fleetContainerLogsReturnsEmptyStringForSuccessfulEmptyLog() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueue(
            GatewayHttpHarness.Reply(
                body = "",
                contentType = "text/plain",
            ),
        )

        val logs = f.client.fleetContainerLogs("spark", "idle")

        assertNotNull(logs)
        assertEquals("", logs)
    }

    @Test
    fun fleetEvalsMapsJudgeRunsCategoriesAndSpeed() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            """{
                "judge":true,
                "runs":{
                    "qwen":{
                        "recipe":"qwen",
                        "node":"spark-a",
                        "model":"Qwen3",
                        "pass":19,
                        "total":20,
                        "score":95.5,
                        "speed":{"ttftMs":123.4,"decodeTokPerSec":47.8},
                        "categories":{"tools":100.0,"language":92.5,"logic":95.0,"stability":94.5}
                    }
                }
            }
            """.trimIndent(),
        )

        val evals = f.client.fleetEvals()

        assertEquals("https://gateway.example/api/v1/fleet/api/evals", f.transport.singleRequest().url)
        assertTrue(evals?.judge == true)
        val run = assertNotNull(evals).runs.getValue("qwen")
        assertEquals("Qwen3", run.model)
        assertEquals(19, run.pass)
        assertEquals(20, run.total)
        assertEquals(95.5, run.score)
        assertEquals(123.4, run.speed?.ttftMs)
        assertEquals(47.8, run.speed?.decodeTokPerSec)
        assertEquals(100.0, run.categories["tools"])
        assertEquals(92.5, run.categories["language"])
        assertEquals(95.0, run.categories["logic"])
        assertEquals(94.5, run.categories["stability"])
    }

    @Test
    fun fleetEvalsCoercesNullRunsToEmptyMap() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"runs":null,"judge":false}""")

        val evals = f.client.fleetEvals()

        assertNotNull(evals)
        assertTrue(evals.runs.isEmpty())
        assertFalse(evals.judge)
    }

    @Test
    fun fleetRunBenchSendsRecipeAndOptionalNodeAndForwardsJob() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"jobId":"eval-1"}""")
        val jobs = mutableListOf<String>()

        val error = f.client.fleetRunBench(
            recipe = "qwen",
            node = "spark-b",
            onJob = jobs::add,
        )

        assertNull(error)
        val request = f.transport.singleRequest()
        assertEquals("https://gateway.example/api/v1/fleet/api/eval", request.url)
        assertEquals("qwen", request.jsonBody?.get("recipe")?.jsonPrimitive?.content)
        assertEquals("spark-b", request.jsonBody?.get("node")?.jsonPrimitive?.content)
        assertEquals(listOf("eval-1"), jobs)
    }

    @Test
    fun fleetRunBenchOmitsBlankNodeSoServerChoosesRecipeNode() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("""{"jobId":"eval-2"}""")

        val error = f.client.fleetRunBench(recipe = "qwen", node = "   ")

        assertNull(error)
        val body = assertNotNull(f.transport.singleRequest().jsonBody)
        assertEquals("qwen", body["recipe"]?.jsonPrimitive?.content)
        assertFalse(body.containsKey("node"))
        assertEquals(1, body.size)
    }

    @Test
    fun fleetRunBenchSurfacesUpstreamValidationError() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            body = "recipe is not running",
            status = HttpStatusCode.UnprocessableEntity,
        )

        val error = f.client.fleetRunBench("stopped", "spark")

        assertEquals("recipe is not running", error)
    }
}
