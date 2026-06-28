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
) {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)

    // Only ever read/written on the main thread (lifecycle callbacks and the
    // main-posted connectivity reconcile), so no synchronization is needed.
    private var foreground = false

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
    }

    private fun onForeground(value: Boolean) {
        foreground = value
        taskScheduler.appInForeground = value
        reconcile()
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
        val sseDesired = online && (foreground || !BACKGROUND_DOZE_ENABLED)
        if (sseDesired) taskScheduler.start() else taskScheduler.stop()

        if (BACKGROUND_DOZE_ENABLED) {
            if (foreground) daemonController.start() else daemonController.stop()
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
