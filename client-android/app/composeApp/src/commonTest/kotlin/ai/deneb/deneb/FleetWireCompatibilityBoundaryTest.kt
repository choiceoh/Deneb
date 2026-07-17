package ai.deneb.deneb

import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class FleetWireCompatibilityBoundaryTest {

    @Test
    fun emptyFleetStateDefaultsToNoNodes() {
        assertEquals(FleetState(), fleetJson.decodeFromString<FleetState>("{}"))
    }

    @Test
    fun goNullNodeArrayCoercesToEmptyList() {
        assertEquals(emptyList(), fleetJson.decodeFromString<FleetState>("""{"nodes":null}""").nodes)
    }

    @Test
    fun unknownStateAndNodeFieldsAreIgnored() {
        val state = fleetJson.decodeFromString<FleetState>(
            """{"updatedAt":"future","nodes":[{"name":"n1","address":"10.0.0.1","future":{"x":1}}]}""",
        )

        assertEquals("n1", state.nodes.single().name)
    }

    @Test
    fun nodeNameRemainsRequired() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetNode>("""{"role":"compute"}""")
        }
    }

    @Test
    fun wrongNodeNameShapeFailsInsteadOfStringifying() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetNode>("""{"name":{"nested":true}}""")
        }
    }

    @Test
    fun nodeDefaultsAreConservative() {
        val node = fleetJson.decodeFromString<FleetNode>("""{"name":"n1"}""")

        assertEquals("", node.role)
        assertFalse(node.reachable)
        assertNull(node.error)
        assertEquals(FleetNodeMetrics(), node.metrics)
        assertEquals(emptyList(), node.models)
    }

    @Test
    fun explicitNullNodeCollectionsAndMetricsCoerceToDefaults() {
        val node = fleetJson.decodeFromString<FleetNode>(
            """{"name":"n1","metrics":null,"models":null}""",
        )

        assertEquals(FleetNodeMetrics(), node.metrics)
        assertEquals(emptyList(), node.models)
    }

    @Test
    fun explicitNullDefaultedNodeScalarsCoerceToDefaults() {
        val node = fleetJson.decodeFromString<FleetNode>(
            """{"name":"n1","role":null,"reachable":null}""",
        )

        assertEquals("", node.role)
        assertFalse(node.reachable)
    }

    @Test
    fun nullableNodeErrorPreservesNullAndText() {
        assertNull(fleetJson.decodeFromString<FleetNode>("""{"name":"n1","error":null}""").error)
        assertEquals("ssh timeout", fleetJson.decodeFromString<FleetNode>("""{"name":"n1","error":"ssh timeout"}""").error)
    }

    @Test
    fun metricsGoNullArraysCoerceIndependently() {
        val metrics = fleetJson.decodeFromString<FleetNodeMetrics>(
            """{"gpus":null,"disks":null,"services":null}""",
        )

        assertEquals(emptyList(), metrics.gpus)
        assertEquals(emptyList(), metrics.disks)
        assertEquals(emptyList(), metrics.services)
        assertNull(metrics.memory)
    }

    @Test
    fun gpuNullableTelemetryStaysNull() {
        val gpu = fleetJson.decodeFromString<FleetGpu>(
            """{"index":2,"utilPct":null,"tempC":null,"powerW":42.1}""",
        )

        assertEquals(2, gpu.index)
        assertNull(gpu.utilPct)
        assertNull(gpu.tempC)
    }

    @Test
    fun gpuOmittedIndexDefaultsZero() {
        assertEquals(0, fleetJson.decodeFromString<FleetGpu>("{}").index)
    }

    @Test
    fun gpuIntegerExtremesRoundTrip() {
        val values = listOf(
            FleetGpu(Int.MIN_VALUE, Int.MIN_VALUE, Int.MAX_VALUE),
            FleetGpu(Int.MAX_VALUE, Int.MAX_VALUE, Int.MIN_VALUE),
        )

        assertEquals(values, fleetJson.decodeFromString<List<FleetGpu>>(fleetJson.encodeToString(values)))
    }

    @Test
    fun memoryLongExtremesRoundTrip() {
        val memory = FleetMemory(totalKB = Long.MAX_VALUE, availableKB = Long.MIN_VALUE)

        assertEquals(memory, fleetJson.decodeFromString<FleetMemory>(fleetJson.encodeToString(memory)))
    }

    @Test
    fun nullMemoryObjectRemainsAbsent() {
        assertNull(fleetJson.decodeFromString<FleetNodeMetrics>("""{"memory":null}""").memory)
    }

    @Test
    fun diskFieldsPreservePathsAndUntrustedPercentages() {
        val disk = fleetJson.decodeFromString<FleetDisk>(
            """{"path":"/mnt/한글 disk","totalKB":9223372036854775807,"usedKB":-1,"usePct":999}""",
        )

        assertEquals("/mnt/한글 disk", disk.path)
        assertEquals(Long.MAX_VALUE, disk.totalKB)
        assertEquals(-1L, disk.usedKB)
        assertEquals(999, disk.usePct)
    }

    @Test
    fun serviceHealthDefaultsToNotOk() {
        val service = fleetJson.decodeFromString<FleetServiceHealth>("""{"name":"vllm"}""")

        assertEquals("vllm", service.name)
        assertFalse(service.ok)
    }

    @Test
    fun modelSizePreservesLongRange() {
        val models = listOf(FleetModel("min", Long.MIN_VALUE), FleetModel("max", Long.MAX_VALUE))

        assertEquals(models, fleetJson.decodeFromString<List<FleetModel>>(fleetJson.encodeToString(models)))
    }

    @Test
    fun nestedCompleteNodeRoundTripsWithoutFieldLoss() {
        val node = FleetNode(
            name = "srv1",
            role = "head",
            reachable = true,
            metrics = FleetNodeMetrics(
                gpus = listOf(FleetGpu(0, 77, 65)),
                memory = FleetMemory(128_000, 64_000),
                disks = listOf(FleetDisk("/", 1_000, 500, 50)),
                services = listOf(FleetServiceHealth("vllm", true)),
            ),
            models = listOf(FleetModel("Qwen", 42_000)),
        )

        assertEquals(node, fleetJson.decodeFromString<FleetNode>(fleetJson.encodeToString(node)))
    }

    @Test
    fun recipeNameRemainsRequired() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetRecipe>("{}")
        }
    }

    @Test
    fun recipeDefaultsRepresentStoppedUnknownPlacement() {
        val recipe = fleetJson.decodeFromString<FleetRecipe>("""{"name":"r"}""")

        assertEquals("", recipe.description)
        assertEquals("", recipe.node)
        assertEquals("", recipe.container)
        assertEquals(0, recipe.port)
        assertNull(recipe.vllm)
        assertEquals(FleetRecipeStatus(), recipe.status)
    }

    @Test
    fun nullRecipeStatusCoercesToDefault() {
        val recipe = fleetJson.decodeFromString<FleetRecipe>("""{"name":"r","status":null}""")

        assertEquals(FleetRecipeStatus(), recipe.status)
    }

    @Test
    fun recipeStatusGoNullScalarsCoerceToDefaults() {
        val status = fleetJson.decodeFromString<FleetRecipeStatus>(
            """{"running":null,"weightsPresent":null,"node":null}""",
        )

        assertFalse(status.running)
        assertFalse(status.weightsPresent)
        assertEquals("", status.node)
    }

    @Test
    fun vllmAllowsIndependentNullableOverrides() {
        val vllm = fleetJson.decodeFromString<FleetVllm>(
            """{"gpuMemoryUtilization":0.875,"maxModelLen":null,"maxNumSeqs":32}""",
        )

        assertEquals(0.875, vllm.gpuMemoryUtilization)
        assertNull(vllm.maxModelLen)
        assertEquals(32, vllm.maxNumSeqs)
    }

    @Test
    fun vllmIntegerExtremesAreNotNarrowed() {
        val vllm = FleetVllm(maxModelLen = Int.MAX_VALUE, maxNumSeqs = Int.MIN_VALUE)

        assertEquals(vllm, fleetJson.decodeFromString<FleetVllm>(fleetJson.encodeToString(vllm)))
    }

    @Test
    fun invalidNonFiniteVllmDoubleIsRejected() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetVllm>("""{"gpuMemoryUtilization":NaN}""")
        }
    }

    @Test
    fun jobIdRemainsRequired() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetJob>("""{"title":"missing id"}""")
        }
    }

    @Test
    fun minimalJobDefaultsTextFields() {
        val job = fleetJson.decodeFromString<FleetJob>("""{"id":"j"}""")

        assertEquals("", job.title)
        assertEquals("", job.state)
        assertEquals("", job.log)
        assertEquals("", job.startedAt)
        assertEquals("", job.endedAt)
    }

    @Test
    fun multilineUnicodeJobLogRoundTrips() {
        val job = FleetJob(
            id = "j",
            title = "다운로드 🚀",
            state = "running",
            log = "line 1\n한글 line 2\u0000end",
            startedAt = "2026-07-11T00:00:00Z",
        )

        assertEquals(job, fleetJson.decodeFromString<FleetJob>(fleetJson.encodeToString(job)))
    }

    @Test
    fun hfModelIdRemainsRequired() {
        assertFailsWith<SerializationException> {
            fleetJson.decodeFromString<FleetHFModel>("""{"downloads":1}""")
        }
    }

    @Test
    fun hfModelDefaultsSupportSparseHubRows() {
        val model = fleetJson.decodeFromString<FleetHFModel>("""{"id":"org/model"}""")

        assertEquals(0L, model.downloads)
        assertEquals(0L, model.likes)
        assertEquals(0L, model.params)
        assertEquals("", model.pipelineTag)
        assertEquals("", model.lastModified)
        assertFalse(model.gated)
    }

    @Test
    fun nullHfDefaultScalarsCoerceRatherThanPoisonSearch() {
        val model = fleetJson.decodeFromString<FleetHFModel>(
            """{"id":"org/model","downloads":null,"likes":null,"params":null,"pipelineTag":null,"lastModified":null,"gated":null}""",
        )

        assertEquals(FleetHFModel("org/model"), model)
    }

    @Test
    fun hfLongCountersPreserveExtremes() {
        val model = FleetHFModel("m", downloads = Long.MAX_VALUE, likes = Long.MIN_VALUE, params = Long.MAX_VALUE)

        assertEquals(model, fleetJson.decodeFromString<FleetHFModel>(fleetJson.encodeToString(model)))
    }

    @Test
    fun hfInfoDefaultsAreSafeWhenUpstreamOmitsMetadata() {
        assertEquals(FleetHFInfo(repo = "r"), fleetJson.decodeFromString<FleetHFInfo>("""{"repo":"r"}"""))
    }

    @Test
    fun hfInfoRoundTripsExtremeSizeAndFileCount() {
        val info = FleetHFInfo("repo", Long.MAX_VALUE, Int.MAX_VALUE, gated = true)

        assertEquals(info, fleetJson.decodeFromString<FleetHFInfo>(fleetJson.encodeToString(info)))
    }

    @Test
    fun diagnosisDefaultsToNoFindings() {
        val diagnosis = fleetJson.decodeFromString<FleetDiagnosis>("{}")

        assertEquals("", diagnosis.container)
        assertEquals("", diagnosis.state)
        assertEquals(emptyList(), diagnosis.findings)
        assertEquals("", diagnosis.llm)
    }

    @Test
    fun nullDiagnosisFindingsCoerceToEmptyList() {
        assertEquals(emptyList(), fleetJson.decodeFromString<FleetDiagnosis>("""{"findings":null}""").findings)
    }

    @Test
    fun findingsPreserveCauseFixAndMatchedEvidence() {
        val finding = FleetFinding("OOM", "lower memory", "CUDA out of memory")
        val diagnosis = FleetDiagnosis("vllm", "exited", listOf(finding), "analysis")

        assertEquals(diagnosis, fleetJson.decodeFromString<FleetDiagnosis>(fleetJson.encodeToString(diagnosis)))
    }

    @Test
    fun driftDefaultsToInSync() {
        val drift = fleetJson.decodeFromString<FleetDrift>("{}")

        assertTrue(drift.inSync)
        assertEquals(emptyList(), drift.diffs)
    }

    @Test
    fun nullDriftDiffsAndFlagCoerceToDefaults() {
        val drift = fleetJson.decodeFromString<FleetDrift>("""{"inSync":null,"diffs":null}""")

        assertTrue(drift.inSync)
        assertEquals(emptyList(), drift.diffs)
    }

    @Test
    fun driftDifferencesPreserveOrderAndUnicode() {
        val drift = FleetDrift("r", "n", "c", inSync = false, diffs = listOf("GPU 0", "메모리 변경"))

        assertEquals(drift, fleetJson.decodeFromString<FleetDrift>(fleetJson.encodeToString(drift)))
    }

    @Test
    fun speedDefaultsToZero() {
        assertEquals(FleetSpeed(), fleetJson.decodeFromString<FleetSpeed>("{}"))
    }

    @Test
    fun evalRunDefaultsSupportPartialBenchmark() {
        val run = fleetJson.decodeFromString<FleetEvalRun>("{}")

        assertEquals("", run.recipe)
        assertEquals(0, run.pass)
        assertEquals(0, run.total)
        assertEquals(0.0, run.score)
        assertNull(run.speed)
        assertEquals(emptyMap(), run.categories)
    }

    @Test
    fun nullEvalCategoriesCoerceToEmptyMap() {
        val run = fleetJson.decodeFromString<FleetEvalRun>("""{"categories":null}""")

        assertEquals(emptyMap(), run.categories)
    }

    @Test
    fun evalRunRoundTripsNestedSpeedAndCategories() {
        val run = FleetEvalRun(
            recipe = "r",
            node = "n",
            model = "m",
            pass = 7,
            total = 10,
            score = 70.5,
            speed = FleetSpeed(ttftMs = 123.4, decodeTokPerSec = 55.6),
            categories = linkedMapOf("tools" to 80.0, "language" to 61.0),
        )

        assertEquals(run, fleetJson.decodeFromString<FleetEvalRun>(fleetJson.encodeToString(run)))
    }

    @Test
    fun evalsNullRunMapCoercesToEmpty() {
        val evals = fleetJson.decodeFromString<FleetEvals>("""{"runs":null,"judge":null}""")

        assertEquals(emptyMap(), evals.runs)
        assertFalse(evals.judge)
    }

    @Test
    fun evalsMapPreservesRecipeKeysAndJudgeFlag() {
        val evals = FleetEvals(
            runs = linkedMapOf("recipe/a" to FleetEvalRun(recipe = "recipe/a", score = 99.9)),
            judge = true,
        )

        val decoded = fleetJson.decodeFromString<FleetEvals>(fleetJson.encodeToString(evals))

        assertEquals(listOf("recipe/a"), decoded.runs.keys.toList())
        assertEquals(99.9, decoded.runs["recipe/a"]?.score)
        assertTrue(decoded.judge)
    }

    @Test
    fun oneMalformedNodeRejectsWholeFleetState() {
        val raw = """{"nodes":[{"name":"good"},{"name":{"bad":true}}]}"""

        assertFailsWith<SerializationException> { fleetJson.decodeFromString<FleetState>(raw) }
    }

    @Test
    fun oneMalformedCategoryValueRejectsWholeEvalRun() {
        val raw = """{"categories":{"tools":"not-a-number"}}"""

        assertFailsWith<SerializationException> { fleetJson.decodeFromString<FleetEvalRun>(raw) }
    }

    @Test
    fun lenientJsonAcceptsUnquotedObjectKeys() {
        val node = fleetJson.decodeFromString<FleetNode>("{name:srv1, role:head, reachable:true}")

        assertEquals("srv1", node.name)
        assertEquals("head", node.role)
        assertTrue(node.reachable)
    }

    @Test
    fun repeatedRoundTripIsStableAcrossNestedFleetState() {
        val state = FleetState(
            nodes = listOf(
                FleetNode("n1", reachable = true),
                FleetNode("n2", error = "offline", models = listOf(FleetModel("m", 7))),
            ),
        )
        val once = fleetJson.decodeFromString<FleetState>(fleetJson.encodeToString(state))
        val twice = fleetJson.decodeFromString<FleetState>(fleetJson.encodeToString(once))

        assertEquals(state, once)
        assertEquals(once, twice)
    }
}
