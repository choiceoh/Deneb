package ai.deneb.data

import java.security.GeneralSecurityException
import java.security.KeyStoreException
import javax.crypto.BadPaddingException

/**
 * Whether a failure to open the encrypted settings store means the stored bytes
 * are genuinely unreadable — the only condition that justifies DELETING it.
 *
 * That distinction matters because the wipe is destructive and silent: deleting
 * `kai_secure_prefs` drops every setting, section cache, bookmark, composer
 * draft and feed-read marker on the device. Only the gateway url+token survive
 * (DurableMirrorSettings mirrors those to a separate plain file on purpose). So
 * a Keystore that is merely *unavailable* — busy, mid-boot, user-locked, a
 * flaky vendor provider — must be retried, not answered by throwing the user's
 * data away.
 *
 * The whole chain is scanned and the most specific finding wins, rather than the
 * outermost — the store's own wrapper is usually a plain
 * [GeneralSecurityException], so first-match would read "keystore was busy" as
 * "file is corrupt" and wipe on a transient failure:
 * - [BadPaddingException] (`AEADBadTagException` is one) anywhere — the keyset
 *   cannot decrypt this file. This is the Auto-Backup-restore case the store was
 *   already guarding: hardware-bound key did not transfer with the file.
 * - else [KeyStoreException] anywhere — the Keystore itself refused or failed.
 *   Says nothing about the stored bytes, so keep them.
 * - else any [GeneralSecurityException] — Tink wraps an undecryptable keyset this
 *   way, so treat it as corruption.
 * - else (`ProviderException`, `IOException`, `IllegalStateException`, …) — not
 *   evidence of corruption; retry instead.
 */
fun isUnreadableSecureStore(cause: Throwable?): Boolean {
    var current = cause
    var depth = 0
    var sawGeneralSecurityFailure = false
    while (current != null && depth < MAX_CAUSE_DEPTH) {
        when (current) {
            is BadPaddingException -> return true
            is KeyStoreException -> return false
            is GeneralSecurityException -> sawGeneralSecurityFailure = true
        }
        current = current.cause
        depth++
    }
    return sawGeneralSecurityFailure
}

private const val MAX_CAUSE_DEPTH = 8
