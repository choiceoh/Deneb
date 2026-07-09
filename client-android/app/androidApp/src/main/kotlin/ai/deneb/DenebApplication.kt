package ai.deneb

import ai.deneb.data.TaskScheduler
import ai.deneb.deneb.DENEB_VERSION_CODE
import ai.deneb.sandbox.sandboxModule
import android.app.Application
import org.koin.android.ext.android.inject
import org.koin.android.ext.koin.androidContext
import org.koin.core.context.startKoin

class DenebApplication : Application() {

    private val taskScheduler: TaskScheduler by inject()
    private val daemonController: DaemonController by inject()

    override fun onCreate() {
        super.onCreate()
        // Install the crash reporter FIRST — before Koin/init — so even a startup
        // crash is captured. It persists the stack and chains to the system handler
        // (crash flow unchanged); MainActivity flushes saved reports to the gateway
        // on the next foreground. See CrashReporter.
        CrashReporter.install(this, DENEB_VERSION_CODE)
        startKoin {
            androidContext(this@DenebApplication)
            modules(appModule, sandboxModule)
        }
        // Battery: the policy owns the app-foreground observer (formerly inline
        // here — it tracks foreground state so the scheduler only raises a tray
        // notification when the in-app banner isn't visible) PLUS the connectivity
        // gate (M2) and the flag-gated background-Doze teardown (M1/M4). See
        // BackgroundConnectionPolicy.
        BackgroundConnectionPolicy(this, taskScheduler, daemonController).install()
    }
}
