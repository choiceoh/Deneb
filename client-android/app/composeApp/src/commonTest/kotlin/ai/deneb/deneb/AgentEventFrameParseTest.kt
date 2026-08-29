package ai.deneb.deneb

import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The spectate surface's wire contract: `event: gateway` SSE frames from the
 * gateway event plane become [AgentEventFrame]s that drive a foreign turn's
 * live chips (DenebClientSessions' foreign-turn watch).
 *
 * The envelopes below are the real ones the gateway emits — publisher.go wraps
 * {kind, sessionKey, runId, seq, payload} and run_hooks.go / run_exec.go fill
 * the inner payload. Two properties matter beyond field mapping: an unaddressed
 * frame must never be attributed to the watched session, and no input may throw
 * — the parser runs inline on the SSE read loop, so a raised exception would
 * take down the whole event stream, not just one frame.
 */
class AgentEventFrameParseTest {

    private val codec = Json { ignoreUnknownKeys = true }

    private fun parse(raw: String) = parseAgentEventFrame(codec, raw)

    @Test
    fun toolStartCarriesThePairingIdAndDetail() {
        val frame = parse(
            """
            {"type":"event","event":"agent.event","payload":{
              "kind":"tool.start","sessionKey":"s-1","runId":"r-1","seq":3,
              "payload":{"tool":"mail","toolUseId":"tu-9","detail":"받은편지함","ts":1}
            }}
            """.trimIndent(),
        )
        assertEquals("tool.start", frame?.kind)
        assertEquals("s-1", frame?.sessionKey)
        assertEquals("mail", frame?.tool)
        // toolUseId pairs start↔end in TurnProgress; losing it strands the row.
        assertEquals("tu-9", frame?.toolUseId)
        assertEquals("받은편지함", frame?.detail)
    }

    @Test
    fun toolEndCarriesTheErrorFlagAndServerAuthoredSummary() {
        val frame = parse(
            """
            {"type":"event","event":"agent.event","payload":{
              "kind":"tool.end","sessionKey":"s-1",
              "payload":{"tool":"web","toolUseId":"tu-9","isError":true,"summary":"3건"}
            }}
            """.trimIndent(),
        )
        assertEquals("tool.end", frame?.kind)
        assertTrue(frame?.isError == true)
        assertEquals("3건", frame?.summary)
    }

    @Test
    fun phaseChangedCarriesTheKoreanLabelVerbatim() {
        // The label is authored server-side (toolport.ChatProgressLabel) so every
        // surface narrates a foreign turn with identical wording.
        val frame = parse(
            """
            {"type":"event","event":"agent.event","payload":{
              "kind":"phase.changed","sessionKey":"s-1",
              "payload":{"phase":"generating","label":"답변 작성 중…"}
            }}
            """.trimIndent(),
        )
        assertEquals("generating", frame?.phase)
        assertEquals("답변 작성 중…", frame?.label)
    }

    @Test
    fun absentInnerFieldsBecomeEmptyRatherThanFailing() {
        // phase.changed has no tool fields and tool frames have no phase; a
        // partial envelope must still parse.
        val frame = parse(
            """{"event":"agent.event","payload":{"kind":"run.end","sessionKey":"s-1"}}""",
        )
        assertEquals("run.end", frame?.kind)
        assertEquals("", frame?.tool)
        assertEquals("", frame?.label)
        assertTrue(frame?.isError == false)
    }

    @Test
    fun onlyAgentEventsAreClaimed() {
        // The same `gateway` SSE channel carries other event names; claiming one
        // would feed a non-turn payload into the chip state machine.
        assertNull(parse("""{"event":"session.message","payload":{"kind":"x","sessionKey":"s-1"}}"""))
        assertNull(parse("""{"event":"config.changed","payload":{}}"""))
    }

    @Test
    fun anUnaddressedFrameIsRejectedRatherThanMisattributed() {
        // Without sessionKey the watch's `frame.sessionKey != key` filter would
        // let a global broadcast render as the watched session's activity.
        assertNull(parse("""{"event":"agent.event","payload":{"kind":"tool.start"}}"""))
        assertNull(parse("""{"event":"agent.event","payload":{"sessionKey":"s-1"}}"""))
        assertNull(parse("""{"event":"agent.event","payload":{"kind":"","sessionKey":"s-1"}}"""))
    }

    @Test
    fun noInputThrows() {
        // Runs inline on the SSE read loop: a throw kills the stream, not a frame.
        for (raw in listOf("", "   ", "not json", "[]", "null", "42", """{"event":"agent.event"}""")) {
            assertNull(parse(raw), "expected null for ${'"'}$raw${'"'}")
        }
    }
}
