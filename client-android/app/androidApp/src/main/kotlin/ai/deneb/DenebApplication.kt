package ai.deneb

import ai.deneb.data.TaskScheduler
import ai.deneb.sandbox.sandboxModule
import android.app.Application
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import org.koin.android.ext.android.inject
import org.koin.android.ext.koin.androidContext
import org.koin.core.context.startKoin

class DenebApplication : Application() {

    private val taskScheduler: TaskScheduler by inject()

    override fun onCreate() {
        super.onCreate()
        startKoin {
            androidContext(this@DenebApplication)
            modules(appModule, sandboxModule)
        }
        // Track app foreground state so the scheduler only pushes a heartbeat notification
        // when the in-app banner isn't visible. ViewModel lifecycle is the wrong signal —
        // it survives backgrounding and only clears on Activity destruction.
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) {
                taskScheduler.appInForeground = true
            }
            override fun onStop(owner: LifecycleOwner) {
                taskScheduler.appInForeground = false
            }
        })
    }
}
