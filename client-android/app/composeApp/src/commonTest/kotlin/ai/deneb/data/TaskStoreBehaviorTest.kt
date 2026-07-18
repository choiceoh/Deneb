package ai.deneb.data

import com.russhwolf.settings.MapSettings
import com.russhwolf.settings.Settings
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue
import kotlin.time.Clock

class TaskStoreBehaviorTest {

    private class ScheduledWriteFailingSettings(
        private val delegate: Settings = MapSettings(),
    ) : Settings by delegate {
        var failWrites = false
        var scheduledReads = 0
        var scheduledWrites = 0
        var beforeScheduledWrite: (() -> Unit)? = null

        override fun getString(key: String, defaultValue: String): String {
            if (key == AppSettings.KEY_SCHEDULED_TASKS) scheduledReads++
            return delegate.getString(key, defaultValue)
        }

        override fun putString(key: String, value: String) {
            if (key == AppSettings.KEY_SCHEDULED_TASKS) {
                beforeScheduledWrite?.invoke()
                if (failWrites) error("scheduled task persistence unavailable")
                scheduledWrites++
            }
            delegate.putString(key, value)
        }
    }

    private fun fixture(initial: List<ScheduledTask> = emptyList()): Pair<AppSettings, TaskStore> {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson(SharedJson.encodeToString(initial))
        return settings to TaskStore(settings)
    }

    private fun task(
        id: String,
        trigger: TaskTrigger = TaskTrigger.TIME,
        status: TaskStatus = TaskStatus.PENDING,
        scheduledAt: Long = 1L,
        cron: String? = null,
        description: String = id,
    ) = ScheduledTask(
        id = id,
        description = description,
        prompt = "prompt-$id",
        scheduledAtEpochMs = scheduledAt,
        createdAtEpochMs = 0L,
        cron = cron,
        trigger = trigger,
        status = status,
    )

    @Test
    fun emptySettingsExposeEmptyViews() {
        val store = TaskStore(AppSettings(MapSettings()))

        assertEquals(emptyList(), store.getAllTasks())
        assertEquals(emptyList(), store.getPendingTasks())
        assertEquals(emptyList(), store.getPendingHeartbeatAdditions())
        assertEquals(emptyList(), store.getDueTasks())
        assertEquals(PendingTaskPartition(emptyList(), emptyList()), store.getPendingTasksPartitioned())
    }

    @Test
    fun malformedStorageFailsClosedWithoutMutatingPayload() {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson("{not-json")

        assertEquals(emptyList(), TaskStore(settings).getAllTasks())
        assertEquals("{not-json", settings.getScheduledTasksJson())
    }

    @Test
    fun nonArrayStorageFailsClosed() {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson("""{"id":"one"}""")

        assertEquals(emptyList(), TaskStore(settings).getAllTasks())
    }

    @Test
    fun unknownFieldsAreIgnoredWhenLoading() {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson(
            """[{"id":"t","description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2,"future":{"x":1}}]""",
        )

        assertEquals("t", TaskStore(settings).getAllTasks().single().id)
    }

    @Test
    fun unknownTriggerCoercesToBackwardCompatibleDefault() {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson(
            """[{"id":"t","description":"d","prompt":"p","scheduledAtEpochMs":1,"createdAtEpochMs":2,"trigger":"FUTURE"}]""",
        )

        assertEquals(TaskTrigger.TIME, TaskStore(settings).getAllTasks().single().trigger)
    }

    @Test
    fun addTimeTaskGeneratesIdentityAndPersistsEveryField() = runTest {
        val (settings, store) = fixture()
        val before = Clock.System.now().toEpochMilliseconds()

        val added = store.addTask("보고서", "작성해", 123_456L)
        val after = Clock.System.now().toEpochMilliseconds()
        val reloaded = TaskStore(settings).getAllTasks().single()

        assertTrue(added.id.isNotBlank())
        assertEquals("보고서", added.description)
        assertEquals("작성해", added.prompt)
        assertEquals(123_456L, added.scheduledAtEpochMs)
        assertTrue(added.createdAtEpochMs in before..after)
        assertEquals(TaskTrigger.TIME, added.trigger)
        assertEquals(added, reloaded)
    }

    @Test
    fun generatedTaskIdsAreUniqueAcrossRapidAdds() = runTest {
        val (_, store) = fixture()

        val ids = (0 until 40).map { store.addTask("same", "same", 1L).id }

        assertEquals(ids.size, ids.toSet().size)
        assertTrue(ids.none { it.isBlank() })
    }

    @Test
    fun heartbeatTaskForcesZeroScheduleAndPreservesCronText() = runTest {
        val (_, store) = fixture()

        val added = store.addTask(
            description = "heartbeat",
            prompt = "check",
            scheduledAtEpochMs = Long.MAX_VALUE,
            cron = "ignored expression",
            trigger = TaskTrigger.HEARTBEAT,
        )

        assertEquals(0L, added.scheduledAtEpochMs)
        assertEquals("ignored expression", added.cron)
        assertEquals(TaskTrigger.HEARTBEAT, added.trigger)
    }

    @Test
    fun cronArgumentSelectsCronTriggerByDefault() = runTest {
        val (_, store) = fixture()

        val added = store.addTask("cron", "run", 55L, cron = "0 9 * * *")

        assertEquals(TaskTrigger.CRON, added.trigger)
        assertEquals(55L, added.scheduledAtEpochMs)
    }

    @Test
    fun zeroScheduleCronComputesANextFireTime() = runTest {
        val (_, store) = fixture()
        val before = Clock.System.now().toEpochMilliseconds()

        val added = store.addTask("cron", "run", 0L, cron = "* * * * *")

        assertTrue(added.scheduledAtEpochMs >= before)
        assertTrue(added.scheduledAtEpochMs <= before + 61_000L)
    }

    @Test
    fun invalidZeroScheduleCronFallsBackToCurrentTime() = runTest {
        val (_, store) = fixture()
        val before = Clock.System.now().toEpochMilliseconds()

        val added = store.addTask("bad cron", "run", 0L, cron = "not a cron")
        val after = Clock.System.now().toEpochMilliseconds()

        assertTrue(added.scheduledAtEpochMs in before..after)
        assertEquals(TaskTrigger.CRON, added.trigger)
    }

    @Test
    fun explicitCronScheduleIsNotRecomputed() = runTest {
        val (_, store) = fixture()

        val added = store.addTask("cron", "run", -99L, cron = "* * * * *")

        assertEquals(-99L, added.scheduledAtEpochMs)
    }

    @Test
    fun pendingScheduledViewFiltersHeartbeatAndCompletedRowsWithoutSorting() {
        val rows = listOf(
            task("future", scheduledAt = Long.MAX_VALUE),
            task("heartbeat", trigger = TaskTrigger.HEARTBEAT, scheduledAt = 0L),
            task("done", status = TaskStatus.COMPLETED),
            task("cron", trigger = TaskTrigger.CRON, cron = "* * * * *"),
        )
        val (_, store) = fixture(rows)

        assertEquals(listOf("future", "cron"), store.getPendingTasks().map { it.id })
    }

    @Test
    fun heartbeatViewReturnsOnlyPendingHeartbeatRows() {
        val rows = listOf(
            task("time"),
            task("active", trigger = TaskTrigger.HEARTBEAT),
            task("done", trigger = TaskTrigger.HEARTBEAT, status = TaskStatus.COMPLETED),
        )
        val (_, store) = fixture(rows)

        assertEquals(listOf("active"), store.getPendingHeartbeatAdditions().map { it.id })
    }

    @Test
    fun partitionPreservesRelativeOrderWithinBothGroups() {
        val rows = listOf(
            task("h1", trigger = TaskTrigger.HEARTBEAT),
            task("t1"),
            task("h2", trigger = TaskTrigger.HEARTBEAT),
            task("done", status = TaskStatus.COMPLETED),
            task("c1", trigger = TaskTrigger.CRON, cron = "* * * * *"),
        )
        val (_, store) = fixture(rows)

        val partition = store.getPendingTasksPartitioned()

        assertEquals(listOf("t1", "c1"), partition.scheduled.map { it.id })
        assertEquals(listOf("h1", "h2"), partition.heartbeatAdditions.map { it.id })
    }

    @Test
    fun partitionReadsPersistedJsonOnlyOnce() {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(SharedJson.encodeToString(listOf(task("t"), task("h", TaskTrigger.HEARTBEAT))))
        raw.scheduledReads = 0

        TaskStore(settings).getPendingTasksPartitioned()

        assertEquals(1, raw.scheduledReads)
    }

    @Test
    fun dueViewIncludesPastAndNegativeTimesOnlyWhenPendingAndScheduled() {
        val now = Clock.System.now().toEpochMilliseconds()
        val rows = listOf(
            task("negative", scheduledAt = Long.MIN_VALUE),
            task("past", scheduledAt = now - 1),
            task("future", scheduledAt = now + 60_000),
            task("done", status = TaskStatus.COMPLETED, scheduledAt = now - 1),
            task("heartbeat", trigger = TaskTrigger.HEARTBEAT, scheduledAt = 0),
        )
        val (_, store) = fixture(rows)

        assertEquals(listOf("negative", "past"), store.getDueTasks().map { it.id })
    }

    @Test
    fun updateReplacesMatchingRowInPlaceAndPersists() = runTest {
        val (settings, store) = fixture(listOf(task("a"), task("b"), task("c")))
        val replacement = task("b", description = "changed", status = TaskStatus.COMPLETED)

        assertEquals(replacement, store.updateTask(replacement))

        val reloaded = TaskStore(settings).getAllTasks()
        assertEquals(listOf("a", "b", "c"), reloaded.map { it.id })
        assertEquals("changed", reloaded[1].description)
        assertEquals(TaskStatus.COMPLETED, reloaded[1].status)
    }

    @Test
    fun updateMissingIdReturnsInputWithoutWriting() = runTest {
        val (settings, store) = fixture(listOf(task("a")))
        val before = settings.getScheduledTasksJson()
        val missing = task("missing", description = "not stored")

        assertEquals(missing, store.updateTask(missing))
        assertEquals(before, settings.getScheduledTasksJson())
        assertEquals(listOf("a"), store.getAllTasks().map { it.id })
    }

    @Test
    fun duplicateStoredIdsUpdateOnlyTheFirstMatch() = runTest {
        val (_, store) = fixture(listOf(task("dup", description = "first"), task("dup", description = "second")))

        store.updateTask(task("dup", description = "replacement"))

        assertEquals(listOf("replacement", "second"), store.getAllTasks().map { it.description })
    }

    @Test
    fun removeExistingIdDeletesEveryDuplicateAndPersists() = runTest {
        val (settings, store) = fixture(listOf(task("dup"), task("keep"), task("dup")))

        assertTrue(store.removeTask("dup"))

        assertEquals(listOf("keep"), TaskStore(settings).getAllTasks().map { it.id })
    }

    @Test
    fun removeMissingIdIsFalseAndDoesNotRewriteStorage() = runTest {
        val (settings, store) = fixture(listOf(task("keep")))
        val before = settings.getScheduledTasksJson()

        assertFalse(store.removeTask("missing"))
        assertEquals(before, settings.getScheduledTasksJson())
    }

    @Test
    fun corruptedStorageIsReplacedWhenANewTaskIsAdded() = runTest {
        val settings = AppSettings(MapSettings())
        settings.setScheduledTasksJson("not-json")
        val store = TaskStore(settings)

        val added = store.addTask("recovered", "prompt", 1L)

        assertEquals(listOf(added), store.getAllTasks())
        assertTrue(settings.getScheduledTasksJson().startsWith("["))
    }

    @Test
    fun noOpMutationPreservesMalformedTaskPayload() = runTest {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson("not-json")
        raw.scheduledWrites = 0
        val store = TaskStore(settings)

        assertFalse(store.removeTask("missing"))

        assertEquals("not-json", settings.getScheduledTasksJson())
        assertEquals(0, raw.scheduledWrites)
    }

    @Test
    fun readerDuringMalformedRecoveryWriteCannotClearIncomingTask() = runTest {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson("not-json")
        raw.scheduledWrites = 0
        val store = TaskStore(settings)
        var readDuringWrite: List<ScheduledTask>? = null
        raw.beforeScheduledWrite = { readDuringWrite = store.getAllTasks() }

        val added = store.addTask("recovered", "prompt", 1L)

        assertEquals(emptyList(), readDuringWrite)
        assertEquals(listOf(added), store.getAllTasks())
        assertEquals(1, raw.scheduledWrites)
    }

    @Test
    fun concurrentAddsAreSerializedWithoutLostUpdates() = runTest {
        val (_, store) = fixture()

        val added = (0 until 80).map { index ->
            async { store.addTask("task-$index", "prompt-$index", index.toLong()) }
        }.awaitAll()

        val persisted = store.getAllTasks()
        assertEquals(80, persisted.size)
        assertEquals(80, persisted.map { it.id }.toSet().size)
        assertEquals((0 until 80).map { "task-$it" }.toSet(), persisted.map { it.description }.toSet())
        assertEquals(added.map { it.id }.toSet(), persisted.map { it.id }.toSet())
    }

    @Test
    fun independentStoreInstanceSeesCommittedChanges() = runTest {
        val settings = AppSettings(MapSettings())
        val writer = TaskStore(settings)

        val added = writer.addTask("shared", "prompt", 42L)
        val reader = TaskStore(settings)

        assertEquals(added, reader.getAllTasks().single())
        assertNotEquals(writer, reader)
    }

    @Test
    fun addPropagatesPersistenceFailureWithoutInventingACommittedRow() = runTest {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(SharedJson.encodeToString(listOf(task("existing"))))
        raw.failWrites = true
        val store = TaskStore(settings)

        assertFailsWith<IllegalStateException> {
            store.addTask("lost", "prompt", 1L)
        }

        raw.failWrites = false
        assertEquals(listOf("existing"), store.getAllTasks().map { it.id })
    }

    @Test
    fun migrationWriteFailureStillReturnsDecodedUpgradedRows() {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(
            """[{"id":"legacy","description":"d","prompt":"p","scheduledAtEpochMs":0,"createdAtEpochMs":0,"cron":"0 9 * * *"}]""",
        )
        raw.failWrites = true

        val loaded = TaskStore(settings).getAllTasks()

        assertEquals(1, loaded.size)
        assertEquals(TaskTrigger.CRON, loaded.single().trigger)
        assertEquals("legacy", loaded.single().id)
    }

    @Test
    fun failedMigrationRewriteRetriesOnTheNextUncontendedRead() {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(
            """[{"id":"legacy","description":"d","prompt":"p","scheduledAtEpochMs":0,"createdAtEpochMs":0,"cron":"0 9 * * *"}]""",
        )
        raw.scheduledWrites = 0
        raw.failWrites = true
        val store = TaskStore(settings)

        assertEquals(TaskTrigger.CRON, store.getAllTasks().single().trigger)
        assertFalse(settings.getScheduledTasksJson().contains("\"trigger\":\"CRON\""))

        raw.failWrites = false
        assertEquals(TaskTrigger.CRON, store.getAllTasks().single().trigger)
        assertTrue(settings.getScheduledTasksJson().contains("\"trigger\":\"CRON\""))
        assertEquals(1, raw.scheduledWrites)
    }

    @Test
    fun noOpUpdatePersistsPendingMigrationExactlyOnce() = runTest {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(
            """[{"id":"legacy","description":"d","prompt":"p","scheduledAtEpochMs":0,"createdAtEpochMs":0,"cron":"0 9 * * *"}]""",
        )
        raw.scheduledWrites = 0
        val store = TaskStore(settings)

        val missing = task("missing")
        assertEquals(missing, store.updateTask(missing))

        assertTrue(settings.getScheduledTasksJson().contains("\"trigger\":\"CRON\""))
        assertEquals(1, raw.scheduledWrites)
        assertEquals(TaskTrigger.CRON, store.getAllTasks().single().trigger)
        assertEquals(1, raw.scheduledWrites)
    }

    @Test
    fun addSupersedesPendingMigrationWithOneCombinedWrite() = runTest {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(
            """[{"id":"legacy","description":"d","prompt":"p","scheduledAtEpochMs":0,"createdAtEpochMs":0,"cron":"0 9 * * *"}]""",
        )
        raw.scheduledWrites = 0
        val store = TaskStore(settings)

        val added = store.addTask("new", "prompt", 1L)

        val persisted = store.getAllTasks()
        assertEquals(listOf("legacy", added.id), persisted.map { it.id })
        assertEquals(TaskTrigger.CRON, persisted.first().trigger)
        assertEquals(1, raw.scheduledWrites)
    }

    @Test
    fun nestedReadDuringMigrationRewriteCannotCauseADuplicateWrite() {
        val raw = ScheduledWriteFailingSettings()
        val settings = AppSettings(raw)
        settings.setScheduledTasksJson(
            """[{"id":"legacy","description":"d","prompt":"p","scheduledAtEpochMs":0,"createdAtEpochMs":0,"cron":"0 9 * * *"}]""",
        )
        raw.scheduledWrites = 0
        val store = TaskStore(settings)
        var nestedRead: List<ScheduledTask>? = null
        raw.beforeScheduledWrite = { nestedRead = store.getAllTasks() }

        val loaded = store.getAllTasks()

        assertEquals(TaskTrigger.CRON, loaded.single().trigger)
        assertEquals(TaskTrigger.CRON, nestedRead?.single()?.trigger)
        assertEquals(1, raw.scheduledWrites)
    }
}
