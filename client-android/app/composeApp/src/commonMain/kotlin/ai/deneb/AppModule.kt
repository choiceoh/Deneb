package ai.deneb

import ai.deneb.contacts.ContactsReader
import ai.deneb.data.AppSettings
import ai.deneb.data.ConversationStorage
import ai.deneb.data.DataRepository
import ai.deneb.data.DurableMirrorSettings
import ai.deneb.data.EmailStore
import ai.deneb.data.HeartbeatManager
import ai.deneb.data.MemoryStore
import ai.deneb.data.RemoteDataRepository
import ai.deneb.data.SmsDraftStore
import ai.deneb.data.SmsStore
import ai.deneb.data.TaskScheduler
import ai.deneb.data.TaskStore
import ai.deneb.data.ToolExecutor
import ai.deneb.data.runMigrations
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.email.EmailPoller
import ai.deneb.mcp.McpServerManager
import ai.deneb.network.Requests
import ai.deneb.sms.SmsPoller
import ai.deneb.sms.SmsReader
import ai.deneb.sms.SmsSender
import ai.deneb.tools.CalendarPermissionController
import ai.deneb.tools.ContactsPermissionController
import ai.deneb.tools.NotificationPermissionController
import ai.deneb.tools.SmsPermissionController
import ai.deneb.tools.SmsSendPermissionController
import ai.deneb.ui.chat.ChatViewModel
import org.koin.core.module.dsl.viewModel
import org.koin.dsl.module

val appModule = module {
    single<CalendarPermissionController> { CalendarPermissionController() }
    single<ContactsPermissionController> { ContactsPermissionController() }
    single<ContactsReader> { ContactsReader() }
    single<NotificationPermissionController> { NotificationPermissionController() }
    single<SmsPermissionController> { SmsPermissionController() }
    single<SmsSendPermissionController> { SmsSendPermissionController() }
    single<SmsReader> { SmsReader() }
    single<SmsSender> { SmsSender() }
    single<AppSettings> {
        // Wrap the encrypted store so the gateway URL + token are mirrored into a
        // plain store that survives the Android encrypted-prefs wipe on app update
        // (see DurableMirrorSettings). createDurableSettings() is null on platforms
        // whose secure store already survives updates, so there we use it directly.
        val secure = createSecureSettings()
        val durable = createDurableSettings()
        val settings = if (durable != null) {
            DurableMirrorSettings(secure, durable, DurableMirrorSettings.GATEWAY_KEYS)
        } else {
            secure
        }
        AppSettings(settings).also {
            it.runMigrations(createLegacySettings())
        }
    }
    single<Requests> {
        Requests()
    }
    single<ConversationStorage> {
        ConversationStorage(get())
    }
    single<ToolExecutor> {
        ToolExecutor()
    }
    single<MemoryStore> {
        MemoryStore(get())
    }
    single<TaskStore> {
        TaskStore(get())
    }
    single<EmailStore> {
        EmailStore(get())
    }
    single<EmailPoller> {
        EmailPoller(get<EmailStore>())
    }
    single<SmsStore> {
        SmsStore(get())
    }
    single<SmsPoller> {
        SmsPoller(get<SmsStore>(), get<SmsReader>())
    }
    single<SmsDraftStore> {
        SmsDraftStore(get())
    }
    single<HeartbeatManager> {
        HeartbeatManager(get(), get(), get(), get())
    }
    single<McpServerManager> {
        McpServerManager(get())
    }
    single<RemoteDataRepository> {
        RemoteDataRepository(
            requests = get(),
            appSettings = get(),
            conversationStorage = get(),
            toolExecutor = get(),
            memoryStore = get(),
            taskStore = get(),
            heartbeatManager = get(),
            emailStore = get(),
            emailPoller = get(),
            smsStore = get(),
            smsPoller = get(),
            smsReader = get(),
            smsPermissionController = get(),
            smsSendPermissionController = get(),
            smsSender = get(),
            smsDraftStore = get(),
            mcpServerManager = get(),
            sandboxController = get(),
        )
    }
    single<DataRepository> { DenebGatewayClient(get<RemoteDataRepository>(), get<AppSettings>()) }
    single<TaskScheduler> {
        // Deneb scheduling, heartbeats, mail polling, and model work live on the
        // gateway. The native app only needs the scheduler shell for the
        // gateway event subscription that backs Android push notifications.
        TaskScheduler(
            get<DataRepository>(),
        )
    }
    single<DaemonController> { createDaemonController() }
    single<SandboxController> { createSandboxController() }
    viewModel { ChatViewModel(get<DataRepository>(), get<TaskScheduler>()) }
}
