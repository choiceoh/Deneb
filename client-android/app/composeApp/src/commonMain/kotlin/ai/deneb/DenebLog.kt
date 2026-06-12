package ai.deneb

/**
 * Minimal multiplatform logger for common code.
 *
 * commonMain previously had no logging facility at all, so user-affecting
 * failures (store load errors, deneb-ui parse errors, scheduler failures)
 * went to bare `println` — invisible on Android (System.out) and impossible
 * to filter anywhere. Route everything through this instead; each platform
 * maps to its native sink (Logcat on Android, stderr on desktop, console
 * elsewhere).
 *
 * Level guide mirrors the gateway's .claude/rules/logging.md:
 *  - [error]: the user observably loses something (data failed to load,
 *    a scheduled task won't run).
 *  - [warn]: degraded but surfaced/recovered (a parse error that falls back
 *    to an error card).
 *  - [debug]: operator tracing only (HTTP wire logs).
 */
expect object DenebLog {
    fun debug(tag: String, message: String)
    fun warn(tag: String, message: String)
    fun error(tag: String, message: String, throwable: Throwable? = null)
}
