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
    fun oneMalformedNodeRejectsWholeFleetState() {
        val raw = """{"nodes":[{"name":"good"},{"name":{"bad":true}}]}"""

        assertFailsWith<SerializationException> { fleetJson.decodeFromString<FleetState>(raw) }
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
