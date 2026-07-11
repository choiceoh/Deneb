package ai.deneb.data

import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.Instant

class ScheduledTaskSerializationTest {

    private val json = Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
    }

    private fun completeTask() = ScheduledTask(
        id = "task-1",
        description = "Morning report",
        prompt = "Summarize overnight changes",
        scheduledAtEpochMs = 1_750_000_000_123L,
        createdAtEpochMs = 1_740_000_000_456L,
        cron = "0 9 * * 1-5",
        trigger = TaskTrigger.CRON,
        status = TaskStatus.COMPLETED,
        lastResult = "done",
        consecutiveFailures = 2,
        recentExecutions = listOf(
            TaskExecutionLogEntry(1_750_000_000_000L, success = false, message = "timeout"),
            TaskExecutionLogEntry(1_749_000_000_000L, success = true, message = null),
        ),
    )

    @Test
    fun completeTaskRoundTripsWithoutFieldLoss() {
        val original = completeTask()

        val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(original))

        assertEquals(original, decoded)
    }

    @Test
    fun minimalLegacyPayloadUsesEveryBackwardCompatibleDefault() {
        val decoded = json.decodeFromString<ScheduledTask>(
            """{"id":"t","description":"d","prompt":"p","scheduledAtEpochMs":7,"createdAtEpochMs":3}""",
        )

        assertNull(decoded.cron)
        assertEquals(TaskTrigger.TIME, decoded.trigger)
        assertEquals(TaskStatus.PENDING, decoded.status)
        assertNull(decoded.lastResult)
        assertEquals(0, decoded.consecutiveFailures)
        assertEquals(emptyList(), decoded.recentExecutions)
    }

    @Test
    fun defaultValuedFieldsAreOmittedByCompactEncoder() {
        val encoded = json.encodeToString(
            ScheduledTask(
                id = "t",
                description = "d",
                prompt = "p",
                scheduledAtEpochMs = 1,
                createdAtEpochMs = 2,
            ),
        )

        assertFalse("cron" in encoded)
        assertFalse("trigger" in encoded)
        assertFalse("status" in encoded)
        assertFalse("lastResult" in encoded)
        assertFalse("consecutiveFailures" in encoded)
        assertFalse("recentExecutions" in encoded)
    }

    @Test
    fun explicitNonDefaultFieldsUseStableWireNames() {
        val encoded = json.encodeToString(completeTask())

        for (name in listOf("scheduledAtEpochMs", "createdAtEpochMs", "trigger", "lastResult", "consecutiveFailures", "recentExecutions")) {
            assertTrue("\"$name\"" in encoded, name)
        }
        assertTrue("\"CRON\"" in encoded)
        assertTrue("\"COMPLETED\"" in encoded)
    }

    @Test
    fun eachTriggerEnumRoundTripsByExactName() {
        for (trigger in TaskTrigger.entries) {
            val encoded = json.encodeToString(trigger)
            assertEquals("\"${trigger.name}\"", encoded)
            assertEquals(trigger, json.decodeFromString<TaskTrigger>(encoded))
        }
    }

    @Test
    fun eachStatusEnumRoundTripsByExactName() {
        for (status in TaskStatus.entries) {
            val encoded = json.encodeToString(status)
            assertEquals("\"${status.name}\"", encoded)
            assertEquals(status, json.decodeFromString<TaskStatus>(encoded))
        }
    }

    @Test
    fun unknownEnumValuesCoerceToPropertyDefaults() {
        val decoded = json.decodeFromString<ScheduledTask>(
            """{"id":"t","description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2,"trigger":"EVENT","status":"PAUSED"}""",
        )

        assertEquals(TaskTrigger.TIME, decoded.trigger)
        assertEquals(TaskStatus.PENDING, decoded.status)
    }

    @Test
    fun missingRequiredIdentityFailsDeserialization() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<ScheduledTask>(
                """{"description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2}""",
            )
        }
    }

    @Test
    fun missingRequiredPromptFailsDeserialization() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<ScheduledTask>(
                """{"id":"t","description":"d","scheduledAtEpochMs":1,"createdAtEpochMs":2}""",
            )
        }
    }

    @Test
    fun wrongPrimitiveTypeFailsInsteadOfStringifying() {
        assertFailsWith<SerializationException> {
            json.decodeFromString<ScheduledTask>(
                """{"id":{"nested":true},"description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2}""",
            )
        }
    }

    @Test
    fun unknownNestedFieldsRemainForwardCompatible() {
        val decoded = json.decodeFromString<ScheduledTask>(
            """{"id":"t","description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2,"serverMetadata":{"owner":"ops"},"future":true}""",
        )

        assertEquals("t", decoded.id)
        assertEquals(TaskTrigger.TIME, decoded.trigger)
    }

    @Test
    fun executionLogRoundTripsNullAndUnicodeMessages() {
        val entries = listOf(
            TaskExecutionLogEntry(0, success = true),
            TaskExecutionLogEntry(-1, success = false, message = "실패 🚨\n두 번째 줄"),
        )

        val decoded = json.decodeFromString<List<TaskExecutionLogEntry>>(json.encodeToString(entries))

        assertEquals(entries, decoded)
    }

    @Test
    fun executionHistoryOrderIsPreservedExactly() {
        val task = completeTask().copy(
            recentExecutions = (0 until 12).map { index ->
                TaskExecutionLogEntry(timestampEpochMs = index.toLong(), success = index % 2 == 0, message = "m$index")
            },
        )

        val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(task))

        assertEquals((0 until 12).map(Int::toLong), decoded.recentExecutions.map { it.timestampEpochMs })
    }

    @Test
    fun serializerDoesNotImplicitlyCapExecutionHistory() {
        val task = completeTask().copy(
            recentExecutions = (0 until 100).map { TaskExecutionLogEntry(it.toLong(), true) },
        )

        val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(task))

        assertEquals(100, decoded.recentExecutions.size)
    }

    @Test
    fun negativeFailureCountIsRepresentedWithoutHiddenNormalization() {
        val task = completeTask().copy(consecutiveFailures = -7)

        val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(task))

        assertEquals(-7, decoded.consecutiveFailures)
    }

    @Test
    fun extremeEpochValuesSurviveSerialization() {
        for (epoch in listOf(Long.MIN_VALUE, -1L, 0L, Long.MAX_VALUE)) {
            val task = completeTask().copy(scheduledAtEpochMs = epoch, createdAtEpochMs = epoch)
            val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(task))

            assertEquals(epoch, decoded.scheduledAtEpochMs)
            assertEquals(epoch, decoded.createdAtEpochMs)
        }
    }

    @Test
    fun scheduledAtConvertsEpochMillisecondsToInstant() {
        val task = completeTask().copy(scheduledAtEpochMs = 1_700_000_000_123L)

        assertEquals(Instant.fromEpochMilliseconds(1_700_000_000_123L), task.scheduledAt)
    }

    @Test
    fun scheduledAtSupportsPreEpochInstants() {
        val task = completeTask().copy(scheduledAtEpochMs = -1L)

        assertEquals(Instant.fromEpochMilliseconds(-1L), task.scheduledAt)
    }

    @Test
    fun copyCanTransitionStatusWithoutLosingScheduleMetadata() {
        val pending = completeTask().copy(status = TaskStatus.PENDING, lastResult = null)
        val completed = pending.copy(status = TaskStatus.COMPLETED, lastResult = "ok")

        assertEquals(pending.id, completed.id)
        assertEquals(pending.cron, completed.cron)
        assertEquals(pending.trigger, completed.trigger)
        assertEquals(TaskStatus.COMPLETED, completed.status)
        assertEquals("ok", completed.lastResult)
    }

    @Test
    fun nullOptionalFieldsAndEmptyStringsRemainDistinct() {
        val task = completeTask().copy(cron = "", lastResult = "")
        val decoded = json.decodeFromString<ScheduledTask>(json.encodeToString(task))

        assertEquals("", decoded.cron)
        assertEquals("", decoded.lastResult)
        assertFalse(decoded.cron == null)
        assertFalse(decoded.lastResult == null)
    }
}
