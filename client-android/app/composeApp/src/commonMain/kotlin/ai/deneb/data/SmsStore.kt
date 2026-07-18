package ai.deneb.data

import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.serializer

class SmsStore(private val appSettings: AppSettings) {

    private val json = SharedJson
    private val syncState = StoredJsonDocument(
        readJson = appSettings::getSmsSyncStateJson,
        writeJson = appSettings::setSmsSyncStateJson,
        defaultValue = ::SmsSyncState,
        decode = { json.decodeFromString<SmsSyncState>(it) },
        encode = { json.encodeToString(it) },
    )
    private val pendingQueue = PendingQueue<SmsMessage, Long>(
        readJson = appSettings::getSmsPendingJson,
        writeJson = appSettings::setSmsPendingJson,
        serializer = ListSerializer(serializer<SmsMessage>()),
        keyOf = { it.id },
    )

    fun getSyncState(): SmsSyncState = syncState.read()

    suspend fun updateSyncState(state: SmsSyncState) = syncState.write(state)

    fun getPending(): List<SmsMessage> = pendingQueue.get()

    suspend fun addPending(messages: List<SmsMessage>) = pendingQueue.add(messages)

    suspend fun removePending(messages: List<SmsMessage>) = pendingQueue.remove(messages)
}
