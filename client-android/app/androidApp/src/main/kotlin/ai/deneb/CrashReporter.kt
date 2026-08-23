package ai.deneb

import ai.deneb.deneb.DenebGatewayClient
import ai.deneb.deneb.ingestEvent
import android.content.Context
import android.os.Build
import android.util.Log
import java.io.File

/**
 * Minimal crash telemetry for the native app, which otherwise has NONE — an
 * uncaught exception just kills the process and the operator never sees the
 * stack (the reason the notification-listener crash took code+timing inference
 * to diagnose instead of a stack trace).
 *
 * At crash time the process is dying, so a network call is unreliable. Instead
 * the handler writes the stack to a small on-disk file synchronously (fsync) and
 * then chains to the previous handler, so the normal crash flow — logcat, the
 * system "app has a bug" dialog, process death — is unchanged. On the NEXT
 * launch, [flushPending] ships the saved reports to the gateway
 * (miniapp.event.ingest, type "client_crash" — logged + stored server-side, no
 * judgment turn) and drops the ones that were delivered.
 */
object CrashReporter {
    private const val TAG = "DenebCrashReporter"
    private const val CRASH_FILE = "pending-crashes.log"
    private const val DELIM = "\n===DENEB-CRASH===\n"

    // Cap retained reports so a crash loop can't grow the file without bound.
    private const val MAX_REPORTS = 20

    // Device label, carried as the report's source so the operator can tell
    // which phone crashed at a glance.
    private val device: String get() = "${Build.MANUFACTURER} ${Build.MODEL}"

    /**
     * Install the global uncaught-exception handler. Call as early as possible
     * (Application.onCreate, before other init) so even a startup crash is caught.
     */
    fun install(context: Context, versionCode: Int) {
        val appContext = context.applicationContext
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, throwable ->
            // The reporter must never turn a crash into a worse crash, so the whole
            // persist step is guarded; then chain so the system still shows its
            // crash dialog and terminates the process exactly as before.
            runCatching { persist(appContext, versionCode, thread, throwable) }
            previous?.uncaughtException(thread, throwable)
        }
    }

    private fun persist(context: Context, versionCode: Int, thread: Thread, throwable: Throwable) {
        val report = buildString {
            append("time=").append(System.currentTimeMillis()).append('\n')
            append("build=").append(versionCode).append('\n')
            append("android=").append(Build.VERSION.SDK_INT).append('\n')
            append("device=").append(device).append('\n')
            append("thread=").append(thread.name).append('\n')
            append(throwable.stackTraceToString())
        }
        val file = File(context.filesDir, CRASH_FILE)
        val existing = runCatching { if (file.exists()) file.readText() else "" }.getOrDefault("")
        // A crash loop writes the same stack over and over, and MAX_REPORTS of one
        // signature pushes out every OTHER crash on the device — exactly the ones
        // worth seeing. Keep the newest copy of a repeated signature (its timestamp
        // is the useful part) rather than N identical ones.
        val signature = crashSignature(report)
        val kept = splitReports(existing).filterNot { crashSignature(it) == signature }
        val reports = (kept + report).takeLast(MAX_REPORTS)
        // Synchronous write + fsync so the report is durable on disk before the
        // process dies (a plain buffered write can be lost when the OS kills us).
        file.outputStream().use { out ->
            out.write(reports.joinToString(DELIM).toByteArray())
            out.flush()
            out.fd.sync()
        }
    }

    /**
     * Ship any saved crash reports to the gateway and drop the delivered ones.
     * Call once the client is available (app foreground). Best-effort: reports
     * that can't be sent (offline / no creds) stay on disk and retry next launch.
     */
    suspend fun flushPending(context: Context, client: DenebGatewayClient) {
        val file = File(context.applicationContext.filesDir, CRASH_FILE)
        if (!file.exists()) return
        val reports = runCatching { file.readText() }.getOrNull()?.let { splitReports(it) }.orEmpty()
        if (reports.isEmpty()) {
            runCatching { file.delete() }
            return
        }
        val undelivered = mutableListOf<String>()
        for (report in reports) {
            val sent = runCatching { client.ingestEvent("client_crash", device, report) }.getOrDefault(false)
            if (!sent) undelivered += report
        }
        // Keep only what we couldn't deliver — nothing delivered is re-sent, nothing
        // undelivered is lost. A crash mid-flush at worst re-sends one report later.
        runCatching {
            if (undelivered.isEmpty()) file.delete() else file.writeText(undelivered.joinToString(DELIM))
        }.onFailure { Log.w(TAG, "crash flush cleanup failed", it) }
    }

    private fun splitReports(raw: String): List<String> = raw.split(DELIM).map { it.trim() }.filter { it.isNotEmpty() }

    /**
     * Identity of a crash, ignoring the parts that differ between repeats.
     *
     * Drops the header (time/build/device vary per occurrence) and keeps the stack,
     * which is what makes two crashes "the same one again".
     */
    internal fun crashSignature(report: String): String = report
        .lineSequence()
        .dropWhile { it.startsWith("time=") || it.startsWith("build=") || it.startsWith("android=") || it.startsWith("device=") || it.startsWith("thread=") }
        .joinToString("\n")
        .trim()
}
