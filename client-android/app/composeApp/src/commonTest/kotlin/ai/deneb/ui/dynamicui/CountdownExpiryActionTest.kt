package ai.deneb.ui.dynamicui

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CountdownExpiryActionTest {
    @Test
    fun inactiveCountdownDoesNotRunExpiryAction() {
        assertFalse(shouldRunCountdownExpiryAction(isInteractive = false, action = CallbackAction(event = "timer_done")))
    }

    @Test
    fun interactiveCountdownRunsOnlyWhenActionExists() {
        assertTrue(shouldRunCountdownExpiryAction(isInteractive = true, action = CallbackAction(event = "timer_done")))
        assertFalse(shouldRunCountdownExpiryAction(isInteractive = true, action = null))
    }
}
