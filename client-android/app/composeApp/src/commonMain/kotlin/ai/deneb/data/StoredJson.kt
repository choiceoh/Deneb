package ai.deneb.data

import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/** Result of classifying one persisted JSON payload before applying a fallback. */
internal sealed interface StoredJsonDecode<out T> {
    data object Absent : StoredJsonDecode<Nothing>

    data class Decoded<T>(val value: T) : StoredJsonDecode<T>

    data class Malformed(val error: Exception) : StoredJsonDecode<Nothing>
}

/** Decode persisted JSON without discarding whether it was absent or malformed. */
internal inline fun <T> decodeStoredJson(
    raw: String,
    decode: (String) -> T,
): StoredJsonDecode<T> {
    if (raw.isEmpty()) return StoredJsonDecode.Absent
    return try {
        StoredJsonDecode.Decoded(decode(raw))
    } catch (error: Exception) {
        StoredJsonDecode.Malformed(error)
    }
}

/** Decode persisted JSON, returning [defaultValue] for absent or malformed data. */
internal inline fun <T> decodeStoredJsonOrDefault(
    raw: String,
    defaultValue: () -> T,
    onMalformed: (Exception) -> Unit = {},
    decode: (String) -> T,
): T = when (val result = decodeStoredJson(raw, decode)) {
    StoredJsonDecode.Absent -> defaultValue()

    is StoredJsonDecode.Decoded -> result.value

    is StoredJsonDecode.Malformed -> {
        runCatching { onMalformed(result.error) }
        defaultValue()
    }
}

/**
 * Read persisted JSON and repair malformed storage only when this reader owns
 * [this] mutex. A reader racing with a mutation still returns a safe default,
 * but cannot clear the mutation's incoming write.
 */
internal inline fun <T> Mutex.loadStoredJsonOrDefault(
    readJson: () -> String,
    clearMalformed: () -> Unit,
    defaultValue: () -> T,
    onMalformed: (Exception) -> Unit = {},
    decode: (String) -> T,
): T {
    val canRepair = tryLock()
    return try {
        decodeStoredJsonOrDefault(
            raw = readJson(),
            defaultValue = defaultValue,
            onMalformed = { error ->
                runCatching { onMalformed(error) }
                if (canRepair) clearMalformed()
            },
            decode = decode,
        )
    } finally {
        if (canRepair) unlock()
    }
}

/** Whether a malformed payload should be removed after a safe fallback is returned. */
internal enum class MalformedStoredJsonPolicy {
    CLEAR,
    PRESERVE,
}

/** A decoded value plus whether its normalized representation should replace the input. */
internal data class StoredJsonMigration<T>(
    val value: T,
    val shouldRewrite: Boolean = false,
)

internal fun <T> unchangedStoredJson(value: T) = StoredJsonMigration(value)

internal fun <T> rewrittenStoredJson(value: T) = StoredJsonMigration(value, shouldRewrite = true)

/** The result of one atomic read-modify-write decision. */
internal sealed interface StoredJsonMutation<out T, out R> {
    data class Persist<T, R>(val value: T, val result: R) : StoredJsonMutation<T, R>

    data class Keep<R>(val result: R) : StoredJsonMutation<Nothing, R>
}

internal fun <T, R> persistStoredJson(value: T, result: R): StoredJsonMutation<T, R> = StoredJsonMutation.Persist(value, result)

internal fun <R> keepStoredJson(result: R): StoredJsonMutation<Nothing, R> = StoredJsonMutation.Keep(result)

/**
 * One JSON-backed document with a single concurrency and recovery contract.
 *
 * Non-suspending reads take the mutex only when it is immediately available.
 * That lets an idle reader clear malformed data or persist a migration, while a
 * reader racing with a mutation can still return a safe snapshot without ever
 * clearing or rewriting the mutation's incoming value. Mutations always own the
 * mutex for the full read-modify-write cycle and propagate their own write
 * failures. Opportunistic repairs remain best-effort.
 */
internal class StoredJsonDocument<T>(
    private val readJson: () -> String,
    private val writeJson: (String) -> Unit,
    private val defaultValue: () -> T,
    private val decode: (String) -> T,
    private val encode: (T) -> String,
    private val malformedPolicy: MalformedStoredJsonPolicy = MalformedStoredJsonPolicy.CLEAR,
    private val migrate: (T) -> StoredJsonMigration<T> = ::unchangedStoredJson,
    private val onMalformed: (Exception) -> Unit = {},
    private val onRepairFailure: (Exception) -> Unit = {},
) {
    private enum class Repair {
        NONE,
        CLEAR,
        REWRITE,
    }

    private data class Snapshot<T>(
        val value: T,
        val repair: Repair,
    )

    private val mutex = Mutex()

    /** Read once and opportunistically repair only when no mutation owns the document. */
    fun read(): T {
        val canRepair = mutex.tryLock()
        return try {
            snapshot().also { if (canRepair) repair(it) }.value
        } finally {
            if (canRepair) mutex.unlock()
        }
    }

    /**
     * Atomically transform the current value. [StoredJsonMutation.Persist]
     * propagates encode/write failures; [StoredJsonMutation.Keep] performs only
     * any pending best-effort malformed cleanup or migration rewrite.
     */
    suspend fun <R> mutate(transform: (T) -> StoredJsonMutation<T, R>): R = mutex.withLock {
        val snapshot = snapshot()
        when (val mutation = transform(snapshot.value)) {
            is StoredJsonMutation.Persist -> {
                writeJson(encode(mutation.value))
                mutation.result
            }

            is StoredJsonMutation.Keep -> {
                repair(snapshot)
                mutation.result
            }
        }
    }

    /** Replace the document without parsing its previous payload. */
    suspend fun write(value: T) = mutex.withLock {
        writeJson(encode(value))
    }

    /** Remove the persisted payload without parsing it. */
    suspend fun clear() = mutex.withLock {
        writeJson("")
    }

    private fun snapshot(): Snapshot<T> = when (val result = decodeStoredJson(readJson(), decode)) {
        StoredJsonDecode.Absent -> Snapshot(defaultValue(), Repair.NONE)

        is StoredJsonDecode.Malformed -> {
            runCatching { onMalformed(result.error) }
            Snapshot(
                value = defaultValue(),
                repair = when (malformedPolicy) {
                    MalformedStoredJsonPolicy.CLEAR -> Repair.CLEAR
                    MalformedStoredJsonPolicy.PRESERVE -> Repair.NONE
                },
            )
        }

        is StoredJsonDecode.Decoded -> {
            val migration = migrate(result.value)
            Snapshot(
                value = migration.value,
                repair = if (migration.shouldRewrite) Repair.REWRITE else Repair.NONE,
            )
        }
    }

    private fun repair(snapshot: Snapshot<T>) {
        try {
            when (snapshot.repair) {
                Repair.NONE -> Unit
                Repair.CLEAR -> writeJson("")
                Repair.REWRITE -> writeJson(encode(snapshot.value))
            }
        } catch (failure: Exception) {
            runCatching { onRepairFailure(failure) }
        }
    }
}
