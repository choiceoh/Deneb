package ai.deneb

import ai.deneb.data.TaskScheduler
import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Handler
import android.os.Looper
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

/**
 * Coordinates when the phone holds its gateway SSE connection and foreground
 * daemon, for battery. It owns three signals — app foreground/background
 * (ProcessLifecycleOwner), network availability (ConnectivityManager) — and
 * drives [TaskScheduler] (the SSE subscription) and [DaemonController] (the
 * foreground service) to the desired state from the main thread.
 *
 * M2 (connectivity-aware reconnect, ACTIVE): when the OS reports no usable
 * network, cancel the SSE subscription so its reconnect/backoff loop stops
 * waking the radio against a dead network; resume when connectivity returns.
 *
 * M1/M4 (background SSE + foreground-service teardown → Doze, ON): dropping the
 * SSE and the foreground service when the app is backgrounded lets the process
 * enter Doze — the large standby-battery win — delegating background proactive
 * delivery to FCM. Enabled ([BACKGROUND_DOZE_ENABLED]=true) by operator decision
 * (battery first). The gateway-side FCM-handoff fixes that make this safe are in
 * place (image/error/fleet FCM fallback + per-mobile predicate); the remaining
 * known edge cases (acknowledged-token gate, native-sync retention/full-refresh,
 * FCM notification-tap deep link, active chat-stream exception — see
 * docs/research/native-app-battery-optimization.md §3.1) are fixed as they
 * surface rather than blocking the battery win. Flip the flag to false to revert
 * to always-connected; the connectivity gate (M2) stays active either way.
 *
 * The single owner of the foreground-state observer: [DenebApplication]
 * installs this instead of its own observer so [TaskScheduler.appInForeground]
 * and the connection lifecycle stay consistent.
 */
class BackgroundConnectionPolicy(
    context: Context,
    private val taskScheduler: TaskScheduler,
    private val daemonController: DaemonController,
    private val chatTurnActive: StateFlow<Boolean>? = null,
    private val fcmDeliveryReady: StateFlow<Boolean>? = null,
) {
    private val appContext = context.applicationContext
    private val mainHandler = Handler(Looper.getMainLooper())
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    // Only ever read/written on the main thread (lifecycle callbacks, the
    // main-posted connectivity reconcile, and the Main-dispatched flow
    // collectors), so no synchronization is needed.
    private var foreground = false
    private var chatStreamActive = false

    // Null flow (non-gateway repository binding) keeps today's behavior.
    private var fcmReady = fcmDeliveryReady?.value ?: true

    /** Registers the lifecycle and connectivity observers. Call once, from onCreate. */
    fun install() {
        ProcessLifecycleOwner.get().lifecycle.addObserver(
            object : DefaultLifecycleObserver {
                override fun onStart(owner: LifecycleOwner) = onForeground(true)
                override fun onStop(owner: LifecycleOwner) = onForeground(false)
            },
        )
        // Default network callbacks arrive on a binder thread; reconcile() is
        // posted to the main thread so TaskScheduler job start/stop never races
        // with the lifecycle callbacks (which are already on the main thread).
        runCatching {
            connectivity.registerDefaultNetworkCallback(
                object : ConnectivityManager.NetworkCallback() {
                    override fun onAvailable(network: Network) = postReconcile()
                    override fun onLost(network: Network) = postReconcile()
                    override fun onCapabilitiesChanged(
                        network: Network,
                        caps: NetworkCapabilities,
                    ) = postReconcile()
                },
            )
        }
        // M1 active-stream exception: track in-flight chat turns so backgrounding
        // mid-turn doesn't tear down the foreground service under the stream.
        // Collected on Main so chatStreamActive stays on the same thread as the
        // lifecycle/connectivity state above.
        chatTurnActive?.let { flow ->
            scope.launch {
                flow.collect { active ->
                    chatStreamActive = active
                    reconcile()
                }
            }
        }
        // Acked-token / server-credential gate (battery doc §3.1): the doze
        // handoff is only safe when the gateway confirmed FCM can DELIVER
        // (token registered AND server credentials loaded). Until then the
        // background teardown would silently drop proactive notifications, so
        // reconcile falls back to always-connected (M3).
        fcmDeliveryReady?.let { flow ->
            scope.launch {
                flow.collect { ready ->
                    fcmReady = ready
                    reconcile()
                }
            }
        }
    }

    private fun onForeground(value: Boolean) {
        foreground = value
        taskScheduler.appInForeground = value
        reconcile()
        // The user is looking at the phone and the radio is coming up anyway —
        // opportune moment to refresh the home widget (throttled, no-op when
        // no widget is placed). Pairs with the stretched system cycle in
        // deneb_widget_info.xml.
        if (value) WidgetRefresher.requestRefresh(appContext)
    }

    private fun postReconcile() {
        mainHandler.post { reconcile() }
    }

    // Runs on the main thread. SSE is held only when there is a usable network
    // (M2) and — once the Doze teardown is enabled (M1/M4) — only while the app
    // is in the foreground (background delivery then rides FCM). With the flag
    // off, connectivity is the only gate and backgrounding keeps today's
    // behavior (SSE stays up while online).
    private fun reconcile() {
        val online = isOnline()
        // Doze teardown is only safe when the FCM handoff is confirmed working
        // (fcmReady). Otherwise keep the SSE in the background — M3 fallback:
        // pre-M1 battery cost, but no silently dropped notifications.
        val dozeSafe = BACKGROUND_DOZE_ENABLED && fcmReady
        val sseDesired = online && (foreground || !dozeSafe)
        if (sseDesired) taskScheduler.start() else taskScheduler.stop()

        if (BACKGROUND_DOZE_ENABLED) {
            when {
                foreground -> daemonController.start()

                // Active-stream exception (battery doc §3.1): stopping the
                // foreground service mid-turn kills the in-flight chat/stream
                // POST's process + network keepalive — the FCM handoff covers
                // proactive events only, not a user-initiated stream. Hold the
                // already-running service; when the turn settles the chat-turn
                // collector re-runs this reconcile and the stop below fires.
                // Never start() from background: Android 12+ blocks background
                // FGS starts, and a turn can only begin while foregrounded.
                chatStreamActive -> Unit

                // FCM delivery unconfirmed: the background SSE kept alive above
                // needs the process to survive — hold the running service too.
                !fcmReady -> Unit

                else -> daemonController.stop()
            }
        }
    }

    private fun isOnline(): Boolean {
        val net = connectivity.activeNetwork ?: return false
        val caps = connectivity.getNetworkCapabilities(net) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
    }

    private companion object {
        // M1/M4 background SSE + foreground-service teardown. ON by operator
        // decision (battery first; accept that background-delivery edge cases —
        // §3.1 🔲 — get fixed as they surface). Flip to false to revert to the
        // prior always-connected behavior (M2 connectivity gating stays either
        // way). Watch: background proactive delivery now rides FCM, so if the
        // gateway lacks FCM credentials it goes silent until reopen — see §3.1.
        const val BACKGROUND_DOZE_ENABLED = true
    }
}
