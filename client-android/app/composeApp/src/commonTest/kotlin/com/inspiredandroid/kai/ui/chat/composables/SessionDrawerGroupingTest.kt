package com.inspiredandroid.kai.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SessionDrawerGroupingTest {

    // Only native-client sessions stay in the chat list.
    @Test
    fun userConversationsAreNotFolded() {
        for (id in listOf("client:main", "client:main:8f3a-uuid", "client")) {
            assertFalse(isSystemSession(id), "expected a user conversation, got folded: $id")
        }
    }

    // Every background/machine session kind the gateway creates folds into the
    // group, so a newly-added kind can never leak into the chat list (the bug that
    // made the grouping look intermittent: it previously caught only cron/system/boot).
    @Test
    fun machineSessionsAreFolded() {
        for (id in listOf(
            "cron:morning-letter:1717",
            "system:email:abc",
            "boot",
            "autonomous:dream:1",
            "curator:weekly",
            "dream:42",
            "genesis:seed",
            "heartbeat:1",
            "hindsight:99",
        )) {
            assertTrue(isSystemSession(id), "expected a machine session to fold: $id")
        }
    }
}
