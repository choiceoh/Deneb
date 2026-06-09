package ai.deneb

interface DaemonController {
    fun start()
    fun stop()
}

expect fun createDaemonController(): DaemonController
