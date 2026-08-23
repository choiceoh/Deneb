package ai.deneb.data

import java.io.IOException
import java.security.GeneralSecurityException
import java.security.KeyStoreException
import java.security.ProviderException
import javax.crypto.AEADBadTagException
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * Which failures may delete the encrypted settings store.
 *
 * The wipe costs every setting, cache, bookmark and draft on the device (only the
 * gateway url+token survive, via the durable mirror), so this classifier is the
 * difference between "recovered from a corrupt file" and "threw the user's data
 * away because the Keystore was briefly busy".
 */
class SecureStoreFailureTest {

    @Test
    fun aeadBadTagMeansTheFileCannotBeDecrypted() {
        // The documented case: Auto Backup restored the file but the hardware-bound
        // key did not come with it.
        assertTrue(isUnreadableSecureStore(AEADBadTagException("tag mismatch")))
        assertTrue(isUnreadableSecureStore(IllegalStateException("open failed", AEADBadTagException())))
    }

    @Test
    fun tinkWrappedKeysetFailuresAlsoCountAsUnreadable() {
        assertTrue(isUnreadableSecureStore(GeneralSecurityException("cannot read keyset")))
    }

    @Test
    fun keystoreUnavailabilityIsTransientAndKeepsTheData() {
        // A Keystore that refused the request says nothing about the stored bytes.
        assertFalse(isUnreadableSecureStore(KeyStoreException("keystore not loaded")))
        // Even when something wraps it in a broader security exception, the more
        // specific KeyStoreException in the chain wins.
        assertFalse(isUnreadableSecureStore(GeneralSecurityException("wrapped", KeyStoreException("busy"))))
    }

    @Test
    fun nonSecurityFailuresAreTransient() {
        assertFalse(isUnreadableSecureStore(ProviderException("vendor provider died")))
        assertFalse(isUnreadableSecureStore(IOException("disk full")))
        assertFalse(isUnreadableSecureStore(IllegalStateException("user not unlocked")))
        assertFalse(isUnreadableSecureStore(null))
    }

    @Test
    fun aCauseCycleDoesNotHangTheClassifier() {
        val a = IllegalStateException("a")
        val b = IllegalStateException("b", a)
        a.initCause(b)

        assertFalse(isUnreadableSecureStore(b))
    }
}
