package ai.deneb.data

import ai.deneb.DenebLog
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

/** Both pending task lists produced by [TaskStore.getPendingTasksPartitioned]. */
data class PendingTaskPartition(
    val scheduled: List<ScheduledTask>,
    val heartbeatAdditions: List<ScheduledTask>,
)

@OptIn(ExperimentalTime::class, ExperimentalUuidApi::class)
class TaskStore(private val appSettings: AppSettings) {

    private val json = SharedJson
    private val tasks = StoredJsonDocument(
        readJson = appSettings::getScheduledTasksJson,
        writeJson = appSettings::setScheduledTasksJson,
        defaultValue = { emptyList() },
        decode = { json.decodeFromString<List<ScheduledTask>>(it) },
        encode = { json.encodeToString(it) },
        malformedPolicy = MalformedStoredJsonPolicy.PRESERVE,
        migrate = ::migrateLegacyCronTasks,
        onMalformed = { DenebLog.error("TaskStore", "failed to load tasks: ${it.message}") },
        onRepairFailure = { DenebLog.error("TaskStore", "failed to persist task migration: ${it.message}") },
    )

    /**
     * Tasks persisted before `trigger` existed decode as TIME. A row carrying a
     * cron expression is actually CRON and is normalized on the first uncontended
     * read. [StoredJsonDocument] owns that rewrite, so it cannot overwrite a
     * concurrent mutation from this store instance.
     */
    private fun migrateLegacyCronTasks(decoded: List<ScheduledTask>): StoredJsonMigration<List<ScheduledTask>> {
        var migrated = false
        val upgraded = decoded.map { task ->
            if (task.trigger == TaskTrigger.TIME && task.cron != null) {
                migrated = true
                task.copy(trigger = TaskTrigger.CRON)
            } else {
                task
            }
        }
        return if (migrated) rewrittenStoredJson(upgraded) else unchangedStoredJson(decoded)
    }

    suspend fun addTask(
        description: String,
        prompt: String,
        scheduledAtEpochMs: Long,
        cron: String? = null,
        trigger: TaskTrigger = if (cron != null) TaskTrigger.CRON else TaskTrigger.TIME,
    ): ScheduledTask = tasks.mutate { current ->
        val now = Clock.System.now()
        val effectiveScheduledAt = when (trigger) {
            TaskTrigger.HEARTBEAT -> 0L

            // heartbeat tasks are not time-gated
            TaskTrigger.CRON -> if (scheduledAtEpochMs == 0L) {
                try {
                    CronExpression(cron!!).nextAfter(now)?.toEpochMilliseconds() ?: now.toEpochMilliseconds()
                } catch (_: Exception) {
                    now.toEpochMilliseconds()
                }
            } else {
                scheduledAtEpochMs
            }

            TaskTrigger.TIME -> scheduledAtEpochMs
        }
        val task = ScheduledTask(
            id = Uuid.random().toString(),
            description = description,
            prompt = prompt,
            scheduledAtEpochMs = effectiveScheduledAt,
            createdAtEpochMs = now.toEpochMilliseconds(),
            cron = cron,
            trigger = trigger,
        )
        persistStoredJson(current + task, task)
    }

    fun getAllTasks(): List<ScheduledTask> = tasks.read()

    /**
     * All PENDING non-heartbeat tasks — what the user thinks of as "scheduled". Heartbeat-
     * triggered tasks are surfaced separately via [getPendingHeartbeatAdditions].
     */
    fun getPendingTasks(): List<ScheduledTask> = tasks.read().filter { it.status == TaskStatus.PENDING && it.trigger != TaskTrigger.HEARTBEAT }

    /** Standing additions to every heartbeat self-check. */
    fun getPendingHeartbeatAdditions(): List<ScheduledTask> = tasks.read().filter { it.status == TaskStatus.PENDING && it.trigger == TaskTrigger.HEARTBEAT }

    /**
     * Both pending scheduled tasks and heartbeat additions from a single load. Hot-path
     * callers (chat system prompt, heartbeat prompt) need both lists per invocation;
     * combining avoids re-parsing the tasks JSON twice.
     */
    fun getPendingTasksPartitioned(): PendingTaskPartition {
        val (additions, scheduled) = tasks.read()
            .filter { it.status == TaskStatus.PENDING }
            .partition { it.trigger == TaskTrigger.HEARTBEAT }
        return PendingTaskPartition(scheduled = scheduled, heartbeatAdditions = additions)
    }

    suspend fun updateTask(task: ScheduledTask): ScheduledTask = tasks.mutate { current ->
        val index = current.indexOfFirst { it.id == task.id }
        if (index < 0) return@mutate keepStoredJson(task)
        val updated = current.toMutableList()
        updated[index] = task
        persistStoredJson(updated, task)
    }

    suspend fun removeTask(id: String): Boolean = tasks.mutate { current ->
        val updated = current.filterNot { it.id == id }
        if (updated.size == current.size) keepStoredJson(false) else persistStoredJson(updated, true)
    }

    fun getDueTasks(): List<ScheduledTask> {
        val now = Clock.System.now().toEpochMilliseconds()
        return tasks.read().filter {
            it.trigger != TaskTrigger.HEARTBEAT &&
                it.scheduledAtEpochMs <= now &&
                it.status == TaskStatus.PENDING
        }
    }
}
