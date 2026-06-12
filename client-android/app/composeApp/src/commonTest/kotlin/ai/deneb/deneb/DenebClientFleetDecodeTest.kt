package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * SparkFleet is a Go server: nil slices/maps marshal as JSON null, so a node
 * that is unreachable (or simply has nothing to report) arrives with
 * "gpus": null / "services": null / "nfs": null. Decoding must coerce those to
 * defaults instead of failing the whole state payload — the original bug left
 * the 노드 tab stuck on "불러오는 중" because one null array poisoned the decode.
 * The fixture mirrors the live /api/state response captured on 2026-06-12.
 */
class DenebClientFleetDecodeTest {
    @Test
    fun decodesStateWithGoNullArrays() {
        val raw = """
        {
          "nodes": [
            {"name":"gx10","role":"head","address":"100.105.145.6","reachable":true,
             "metrics":{"gpus":[{"index":0,"utilPct":37,"tempC":62,"powerW":41.2}],
                        "memory":{"totalKB":127541248,"availableKB":13917184},
                        "disks":[{"path":"/","totalKB":914415616,"usedKB":501841920,"availKB":366039040,"usePct":58}],
                        "services":[{"name":"vllm-nex","url":"http://127.0.0.1:8002/v1/models","ok":false,"httpStatus":0}],
                        "nfs":[{"path":"/mnt/spark4tb","status":"mounted"}]},
             "models":[{"name":"Qwen3.6-35B-A3B-FP8","sizeBytes":37493015668}]},
            {"name":"spark4tb","role":"storage","address":"100.125.220.117","reachable":true,
             "metrics":{"gpus":[{"index":0,"utilPct":null,"tempC":null,"powerW":null}],
                        "memory":{"totalKB":127541248,"availableKB":80000000},
                        "disks":[],"services":[],"nfs":null}},
            {"name":"srv3","role":"compute","address":"100.99.1.1","reachable":false,
             "error":"ssh: connect timed out",
             "metrics":{"gpus":null,"memory":null,"disks":null,"services":null,"nfs":null}}
          ],
          "updatedAt":"2026-06-12T11:30:00+09:00","pollMs":5000
        }
        """.trimIndent()

        val st = fleetJson.decodeFromString<FleetState>(raw)
        assertEquals(3, st.nodes.size)
        assertEquals("gx10", st.nodes[0].name)
        assertEquals(37, st.nodes[0].metrics.gpus.first().utilPct)
        // null arrays / objects coerce to defaults instead of failing the decode
        val srv3 = st.nodes[2]
        assertTrue(!srv3.reachable)
        assertEquals(emptyList(), srv3.metrics.gpus)
        assertEquals(emptyList(), srv3.metrics.services)
        assertEquals(null, srv3.metrics.memory)
        assertEquals("ssh: connect timed out", srv3.error)
        // a null scalar inside a present gpu entry stays null, not a crash
        assertEquals(null, st.nodes[1].metrics.gpus.first().utilPct)
    }

    @Test
    fun decodesRecipesAndJobs() {
        val recipes = fleetJson.decodeFromString<List<FleetRecipe>>(
            """[{"name":"qwen36-fast","description":"","node":"gx10","port":8000,
                 "tags":["vllm"],"status":{"running":true,"weightsPresent":true,"node":"gx10","headroomGB":13.2}}]""",
        )
        assertTrue(recipes.single().status.running)

        val jobs = fleetJson.decodeFromString<List<FleetJob>>(
            """[{"id":"job-3","title":"hf download a/b → gx10","state":"running",
                 "log":"progress: 12G downloaded","startedAt":"2026-06-12T10:00:00+09:00","cmd":"…"}]""",
        )
        assertEquals("job-3", jobs.single().id)
    }
}
