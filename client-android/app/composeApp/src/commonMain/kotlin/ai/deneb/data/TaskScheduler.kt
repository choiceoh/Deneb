package ai.deneb.data

import ai.deneb.DenebLog
import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.subscribeEvents
import ai.deneb.getBackgroundDispatcher
import ai.deneb.sendProactiveReportNotification
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineName
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlin.concurrent.Volatile
import kotlin.coroutines.CoroutineContext

/**
 * The native app's scheduler shell. Deneb scheduling, heartbeats, mail polling,
 * and model work all live on the gateway — the only thing the client needs is the
 * gateway event subscription that backs Android push notifications.
 *
 * The legacy on-device poll loop (which ran heartbeats and scheduled tasks through
 * the removed cloud-direct `askWithTools` path, plus on-device email/SMS polling)
 * is gone: in this gateway-backed build it was never wired with a TaskStore /
 * AppSettings (see AppModule), so it always returned early before doing anything.
 *
 * [enabled] is honored so tests can construct an inert scheduler.
 */
class TaskScheduler(
    private val dataRepository: DataRepository,
    private val enabled: Boolean = true,
    private val backgroundDispatcher: CoroutineContext = getBackgroundDispatcher(),
) {
    /**
     * Process-lifetime scope. Decoupled from any caller's scope so the gateway
     * subscriptions keep firing when a short-lived caller (e.g.
     * `ChatViewModel.viewModelScope`) is cancelled — as long as the OS keeps the
     * process alive (on Android that means `DaemonService` holding a foreground
     * notification).
     */
    // A CoroutineExceptionHandler on THIS scope is load-bearing, not hygiene.
    // Backgrounding cancels the in-flight gateway SSE (stop()), and ktor's request
    // cleanup closing the okhttp response can raise `IllegalStateException:
    // Unbalanced enter/exit` as a CompletionHandlerException. Without a handler here,
    // kotlinx.coroutines routes that straight to the thread's UNCAUGHT handler
    // (handleCoroutineException → Thread.uncaughtExceptionHandler) and kills the app
    // on the background transition — confirmed via the crash reporter (build 611,
    // Android 16, 5/5 identical). This is why the earlier runCatching around cancel()
    // (below) did NOT fix it: nothing is thrown out of cancel(); the exception is
    // delivered to the uncaught handler. The jobs are being cancelled regardless, so
    // swallow + log. Declared before schedulerScope so it's initialized first.
    private val schedulerExceptionHandler = CoroutineExceptionHandler { _, e ->
        DenebLog.warn("TaskScheduler", "scheduler coroutine exception ignored: ${e.message}")
    }

    private val schedulerScope = CoroutineScope(
        SupervisorJob() + backgroundDispatcher + CoroutineName("TaskScheduler") + schedulerExceptionHandler,
    )

    /**
     * Whether the app is currently in the foreground (the user can see the in-app
     * banner). On Android this mirrors `ProcessLifecycleOwner` — set true on the
     * first Activity start, false when all activities stop. Other platforms leave
     * it at the default since their notification actuals are no-ops anyway.
     *
     * When a proactive report arrives and this is `false`, the scheduler escalates
     * to a push notification instead of relying on the (invisible) in-app banner.
     */
    @Volatile
    var appInForeground: Boolean = false

    private var pushJob: Job? = null

    /**
     * Long-lived subscription to the gateway's proactive-event SSE stream. The
     * gateway pushes a frame the moment a 업무-topic report is produced. Only
     * meaningful for the gateway-backed client; on other repositories
     * subscribeEvents isn't available so this stays a no-op.
     *
     * The SSE frame is only a wake-signal: subscribeEvents already kicks a
     * native-sync pull, and that path ([startProactiveNotifications]) raises the
     * notification for any genuinely-new work-feed item — durable and deduped by
     * the persisted sync cursor. Foreground is the only case left here: suppress
     * the tray notification and live-refresh the in-app feed instead.
     */
    private fun startPushSubscription() {
        val gateway = dataRepository as? DenebGatewayClient ?: return
        if (pushJob?.isActive == true) return
        pushJob = schedulerScope.launch {
            gateway.subscribeEvents { _, _ ->
                if (appInForeground) {
                    dataRepository.onProactiveReportForeground()
                }
            }
        }
    }

    private var proactiveJob: Job? = null

    /**
     * Durable proactive-notification path. The gateway's SSE push is best-effort
     * (no persistence), so rather than notify off the live frame we notify off the
     * native-sync stream — [DenebGatewayClient.proactiveNotifications] emits once
     * per genuinely-new work-feed item the cursor-based pull surfaces. The
     * persisted cursor guarantees exactly-once, and the first post-launch sync is
     * treated as catch-up and suppressed so opening the app doesn't fire a barrage.
     */
    private fun startProactiveNotifications() {
        val gateway = dataRepository as? DenebGatewayClient ?: return
        if (proactiveJob?.isActive == true) return
        proactiveJob = schedulerScope.launch {
            gateway.proactiveNotifications.collect { report ->
                // Raise a tray notification only when the user won't see the in-app
                // feed update (backgrounded). Foreground reports refresh the feed
                // live via onProactiveReportForeground.
                if (!appInForeground) {
                    sendProactiveReportNotification(
                        title = report.title,
                        body = report.body,
                        kind = report.kind,
                        ref = report.ref,
                        approveActionId = report.approveActionId,
                        rejectActionId = report.rejectActionId,
                    )
                }
            }
        }
    }

    /**
     * Starts the gateway event subscriptions on the internal long-lived scope.
     * Idempotent — repeated calls (e.g. from both `DaemonService.onCreate` and
     * `ChatViewModel.init`) are no-ops once the subscriptions are running.
     */
    fun start() {
        if (!enabled) return
        startPushSubscription()
        startProactiveNotifications()
    }

    /**
     * Cancels the gateway event subscriptions, dropping the SSE connection so a
     * backgrounded process can enter Doze (battery M1) and so a dead-network
     * reconnect loop stops waking the radio (M2). Idempotent — [start]
     * re-establishes the jobs on foreground/connectivity return. The long-lived
     * scope itself is preserved (reused by the next start); only its child jobs
     * are cancelled. Must be driven from a single thread (the coordinator posts
     * to the main thread) since the job fields are not synchronized.
     */
    fun stop() {
        // Secondary guard for the rare case cancel() DOES throw synchronously. The
        // primary fix for the okhttp `Unbalanced enter/exit` teardown crash is the
        // scope's CoroutineExceptionHandler (see schedulerExceptionHandler) — that
        // exception is delivered to the uncaught handler, not thrown out of cancel(),
        // so runCatching alone was insufficient (build 611 still crashed).
        runCatching { pushJob?.cancel() }
        pushJob = null
        runCatching { proactiveJob?.cancel() }
        proactiveJob = null
    }
}
